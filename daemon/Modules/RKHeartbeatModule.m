// RKHeartbeatModule — 示例模块，新建模块时复制此文件。
//
// 总开关打开且本模块 "on" 设置为 true 时，每 interval_s 秒（默认 10）计一次心跳。覆盖模块 API 的各部分：
//   * 持久化设置（"on"）           → -settingForKey: / -setSetting:
//   * 跟随总开关的工作             → -serviceDidStart / -serviceWillStop
//   * 给 app 的状态                → -moduleStatus（"status" 命令）
//   * 来自 app 的命令              → "set" {on: Bool}、"reset"
//   * 向 app 推送变化              → -postChange
//   * 服务端远程访问               → +remoteCommands
//   * 服务端下发的配置（interval_s） → +configSchema / -config: / -configDidChange:
//
// app 端：app/Features/RKHeartbeatFeature.m。

#import "RKModule.h"
#import "../../common/RKProtocol.h"

@interface RKHeartbeatModule : NSObject <RKModule>
@end

@implementation RKHeartbeatModule {
    RKModuleContext *_ctx;
    dispatch_source_t _timer;
    uint64_t _beats;
    NSDate *_since;
}

RK_REGISTER_MODULE()

+ (NSString *)moduleIdentifier { return @"example.heartbeat"; }

// 对服务端开放（net.link "invoke"）。不实现则模块仅限本地。
+ (NSArray<NSString *> *)remoteCommands { return @[ @"status", @"set", @"reset" ]; }

// 远程命令的说明：开放 API 据此为每个命令生成独立接口与文档。
+ (NSDictionary<NSString *, NSDictionary *> *)commandSchema {
    NSArray *result = @[
        @{ @"key": @"on", @"type": @"bool", @"desc": @"是否开启计数" },
        @{ @"key": @"running", @"type": @"bool", @"desc": @"是否正在计数（开启且常驻服务运行中）" },
        @{ @"key": @"beats", @"type": @"int", @"desc": @"已计次数" },
        @{ @"key": @"interval", @"type": @"int", @"desc": @"计数间隔（秒）" },
        @{ @"key": @"since", @"type": @"number", @"desc": @"本轮开始计数的时间（Unix 秒）" },
    ];
    return @{
        @"status": @{ @"label": @"查询状态", @"desc": @"心跳计数的当前状态", @"result": result },
        @"set": @{ @"label": @"开关计数", @"desc": @"开启或关闭心跳计数",
                   @"params": @[ @{ @"key": @"on", @"type": @"bool", @"label": @"启用计数", @"required": @YES,
                                    @"desc": @"true 开启，false 关闭" } ],
                   @"result": result },
        @"reset": @{ @"label": @"计数清零", @"desc": @"把计数归零，返回清零后的状态", @"result": result },
    };
}

// 可由服务端下发的配置。
+ (NSArray<NSDictionary *> *)configSchema {
    return @[ @{ @"key": @"interval_s", @"type": @"int", @"default": @10, @"min": @2, @"max": @3600,
                 @"label": @"计数间隔（秒）" } ];
}

- (instancetype)initWithContext:(RKModuleContext *)context {
    if ((self = [super init])) _ctx = context;
    return self;
}

- (int64_t)interval { return [[_ctx config:@"interval_s"] longLongValue]; }

// 配置变化立即生效：调整正在运行的 timer。
- (void)configDidChange:(NSDictionary *)changed {
    if (!_timer) return;
    dispatch_source_set_timer(_timer, dispatch_time(DISPATCH_TIME_NOW, self.interval * NSEC_PER_SEC),
                              self.interval * NSEC_PER_SEC, 2 * NSEC_PER_SEC);
    [_ctx postChange];
}

- (BOOL)isOn { return [[_ctx settingForKey:@"on"] boolValue]; }   // 默认关闭

// 唯一决定 timer 是否运行的地方。
- (void)apply {
    BOOL shouldRun = _ctx.serviceEnabled && self.isOn;
    if (shouldRun && !_timer) {
        _since = [NSDate date];
        _timer = dispatch_source_create(DISPATCH_SOURCE_TYPE_TIMER, 0, 0, _ctx.queue);
        dispatch_source_set_timer(_timer,
            dispatch_time(DISPATCH_TIME_NOW, self.interval * NSEC_PER_SEC),
            self.interval * NSEC_PER_SEC, 2 * NSEC_PER_SEC);   // leeway 省电
        __weak typeof(self) weakSelf = self;
        dispatch_source_set_event_handler(_timer, ^{
            typeof(self) self_ = weakSelf;
            if (!self_) return;
            self_->_beats++;
            [self_->_ctx postChange];
        });
        dispatch_resume(_timer);
        os_log(_ctx.log, "started");
    } else if (!shouldRun && _timer) {
        dispatch_source_cancel(_timer);
        _timer = nil;
        os_log(_ctx.log, "stopped after %llu beats", _beats);
    }
}

- (void)serviceDidStart { [self apply]; }
- (void)serviceWillStop { [self apply]; }

- (NSDictionary *)moduleStatus {
    NSMutableDictionary *s = [@{ @"on": @(self.isOn),
                                 @"running": @(_timer != nil),
                                 @"beats": @(_beats),
                                 @"interval": @(self.interval) } mutableCopy];
    if (_since) s[@"since"] = _since;
    return s;
}

- (void)handleCommand:(NSString *)command
            arguments:(NSDictionary *)args
                reply:(RKModuleReply)reply {
    if ([command isEqualToString:@"set"]) {
        id on = args[@"on"];
        if (![on isKindOfClass:NSNumber.class]) {
            reply(nil, [_ctx errorWithCode:RKErrorInvalidArguments description:@"缺少参数 on"]);
            return;
        }
        [_ctx setSetting:on forKey:@"on"];
        [self apply];
        [_ctx postChange];
        reply(self.moduleStatus, nil);
    } else if ([command isEqualToString:@"reset"]) {
        _beats = 0;
        _since = _timer ? [NSDate date] : nil;
        [_ctx postChange];
        reply(self.moduleStatus, nil);
    } else {
        reply(nil, [_ctx errorWithCode:RKErrorUnknownCommand
                           description:[@"未知命令 " stringByAppendingString:command]]);
    }
}

@end
