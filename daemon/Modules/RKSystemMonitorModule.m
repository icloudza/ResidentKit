// RKSystemMonitorModule — system.monitor：资源类内置事件（docs/events.md）。
//
// 常驻服务开启期间每 30 秒采样一次（容差 10 秒，便于系统合并唤醒），只在状态变化时发事件：
//   * resource.memory.raised / cleared：系统内存压力进入或离开 warning / critical，raised 同类至少间隔 60 秒
//   * resource.footprint.high：daemon 自身内存超过 jetsam 上限的 75%，每小时最多一次（提前发现泄漏）
// 不开放远程命令。

#import "RKModule.h"
#import "RKSystem.h"
#import "../../common/RKMemorystatus.h"
#import <mach/mach.h>
#import <sys/sysctl.h>

static const double RKRaiseMinGap = 60;           // resource.memory.raised 最短间隔
static const double RKFootprintGap = 3600;        // resource.footprint.high 最短间隔

static NSString *RKPressureName(int level) {
    return level >= 4 ? @"critical" : level >= 2 ? @"warning" : @"normal";
}

@interface RKSystemMonitorModule : NSObject <RKModule>
@end

@implementation RKSystemMonitorModule {
    RKModuleContext *_ctx;
    dispatch_source_t _timer;
    NSString *_pressure;      // 上次观测到的内存压力
    double _raisedAt;         // 最近一次 raised 的时间
    double _abnormalSince;    // 进入异常的时间，用于 cleared 的持续时长
    double _footprintAt;      // 最近一次 footprint.high 的时间
}

RK_REGISTER_MODULE()

+ (NSString *)moduleIdentifier { return @"system.monitor"; }

+ (NSArray<NSDictionary *> *)configSchema {
    return @[
        @{ @"key": @"interval_s", @"type": @"int", @"default": @30, @"min": @10, @"max": @600,
           @"label": @"采样间隔（秒）", @"desc": @"检查内存压力与服务内存的频率" },
        @{ @"key": @"footprint_pct", @"type": @"int", @"default": @75, @"min": @50, @"max": @95,
           @"label": @"服务内存告警线（%）", @"desc": @"占 jetsam 上限的比例，超过时发 resource.footprint.high" },
    ];
}

- (void)configDidChange:(NSDictionary *)changed {
    if (changed[@"interval_s"] && _timer) [self schedule];
}

- (void)schedule {
    uint64_t iv = [[_ctx config:@"interval_s"] unsignedLongLongValue] * NSEC_PER_SEC;
    dispatch_source_set_timer(_timer, dispatch_time(DISPATCH_TIME_NOW, 2 * NSEC_PER_SEC), iv, 10 * NSEC_PER_SEC);
}

- (instancetype)initWithContext:(RKModuleContext *)context {
    if ((self = [super init])) _ctx = context;
    return self;
}

- (void)serviceDidStart {
    if (_timer) return;
    _pressure = nil;   // 重新开始：第一次采样只记录基线，不发事件
    _timer = dispatch_source_create(DISPATCH_SOURCE_TYPE_TIMER, 0, 0, _ctx.queue);
    [self schedule];
    __weak typeof(self) weakSelf = self;
    dispatch_source_set_event_handler(_timer, ^{ [weakSelf sample]; });
    dispatch_resume(_timer);
}

- (void)serviceWillStop {
    if (_timer) dispatch_source_cancel(_timer);
    _timer = nil;
}

- (NSDictionary *)moduleStatus {
    return @{ @"running": @(_timer != nil), @"pressure": _pressure ?: @"" };
}

#pragma mark - 采样

- (void)sample {
    double now = [NSDate date].timeIntervalSince1970;
    [self checkPressure:now];
    [self checkFootprint:now];
}

- (void)checkPressure:(double)now {
    int level = 0;
    size_t len = sizeof(level);
    if (sysctlbyname("kern.memorystatus_vm_pressure_level", &level, &len, NULL, 0) != 0) return;
    NSString *cur = RKPressureName(level);
    NSString *prev = _pressure;
    _pressure = cur;
    if (!prev || [prev isEqualToString:cur]) return;

    if ([cur isEqualToString:@"normal"]) {
        NSMutableDictionary *p = [@{ @"prev": prev } mutableCopy];
        if (_abnormalSince > 0) p[@"duration_s"] = @((long)(now - _abnormalSince));
        _abnormalSince = 0;
        [_ctx emitEvent:@"resource.memory.cleared" level:@"info" payload:p];
        return;
    }
    if ([prev isEqualToString:@"normal"]) _abnormalSince = now;
    if (now - _raisedAt < RKRaiseMinGap) return;   // 来回抖动时不刷屏
    _raisedAt = now;
    NSMutableDictionary *p = [@{ @"level": cur, @"prev": prev } mutableCopy];
    double pct = [self deviceMemoryPct];
    if (pct >= 0) p[@"dev_mem_pct"] = @(round(pct));
    [_ctx emitEvent:@"resource.memory.raised"
              level:[cur isEqualToString:@"critical"] ? @"error" : @"warning" payload:p];
}

- (void)checkFootprint:(double)now {
    if (now - _footprintAt < RKFootprintGap) return;
    rk_memorystatus_memlimit_t ml = {0};
    if (memorystatus_control(MEMORYSTATUS_CMD_GET_MEMLIMIT_PROPERTIES, getpid(), 0, &ml, sizeof(ml)) != 0 ||
        ml.memlimit_active <= 0) return;
    uint64_t limit = (uint64_t)ml.memlimit_active * 1024 * 1024;
    uint64_t fp = RKFootprintBytes();
    if (fp < limit * [[_ctx config:@"footprint_pct"] doubleValue] / 100) return;
    _footprintAt = now;
    [_ctx emitEvent:@"resource.footprint.high" level:@"warning"
            payload:@{ @"footprint_bytes": @(fp), @"limit_bytes": @(limit),
                       @"pct": @(round((double)fp / limit * 100)) }];
}

// 整机内存占用（活跃 + 联动 + 压缩），失败返回 -1。
- (double)deviceMemoryPct {
    uint64_t total = 0;
    size_t len = sizeof(total);
    if (sysctlbyname("hw.memsize", &total, &len, NULL, 0) != 0 || !total) return -1;
    vm_statistics64_data_t vm = {0};
    mach_msg_type_number_t cnt = HOST_VM_INFO64_COUNT;
    mach_port_t host = mach_host_self();
    kern_return_t kr = host_statistics64(host, HOST_VM_INFO64, (host_info64_t)&vm, &cnt);
    mach_port_deallocate(mach_task_self(), host);
    if (kr != KERN_SUCCESS) return -1;
    uint64_t used = ((uint64_t)vm.active_count + vm.wire_count + vm.compressor_page_count) * vm_kernel_page_size;
    return (double)used / total * 100;
}

@end
