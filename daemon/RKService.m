// RKService.m — 说明见头文件。

#import "RKService.h"
#import "RKMetrics.h"
#import "RKModuleHost.h"
#import "RKSystem.h"
#import "RKWatchdog.h"
#import <notify.h>
#import <sys/stat.h>
#import <os/log.h>

static os_log_t RKLog(void) {
    static os_log_t log;
    static dispatch_once_t once;
    dispatch_once(&once, ^{ log = os_log_create(RK_LOG_SUBSYSTEM, "service"); });
    return log;
}

@implementation RKService {
    BOOL _enabled;
    NSDate *_startedAt;
    NSString *_statePath;
    RKModuleHost *_modules;
}

- (instancetype)init {
    if ((self = [super init])) {
        // WORK_ITEM：每个 block 后 drain autorelease 对象，客户端轮询数小时也不会涨内存。
        _queue = dispatch_queue_create("com.icloudza.residentd.state",
            dispatch_queue_attr_make_with_autorelease_frequency(
                DISPATCH_QUEUE_SERIAL, DISPATCH_AUTORELEASE_FREQUENCY_WORK_ITEM));
        _startedAt = [NSDate date];
        _statePath = RKStatePath();

        [self secureStateDirectory];
        NSDictionary *saved = [NSDictionary dictionaryWithContentsOfFile:_statePath];
        _enabled = saved[@"enabled"] ? [saved[@"enabled"] boolValue] : YES;

        __weak typeof(self) weakSelf = self;
        _modules = [[RKModuleHost alloc] initWithQueue:_queue
                                              settings:saved[@"modules"]
                                              dataRoot:[[_statePath stringByDeletingLastPathComponent]
                                                           stringByAppendingPathComponent:@"modules"]
                                               persist:^{ [weakSelf saveState]; }];
        os_log(RKLog(), "state %{public}@: enabled=%d modules=%{public}@",
               _statePath, _enabled, [_modules.moduleIdentifiers componentsJoinedByString:@","]);
    }
    return self;
}

- (void)start {
    NSDictionary *run = RKRunRecordBegin();
    dispatch_async(_queue, ^{
        [self saveState];   // 记下模块加载时设定的默认值
        [self->_modules setServiceEnabled:self->_enabled];
        // 启动事件：被杀后重新拉起记为 warning（docs/events.md）。
        NSMutableDictionary *p = [run mutableCopy];
        p[@"version"] = @(RK_VERSION);
        [self->_modules emitEvent:@"system.daemon.started"
                            level:[run[@"reason"] isEqual:@"respawn"] ? @"warning" : @"info"
                          payload:p];
    });
}

// state 目录存有机密（服务端 token、证书 pin）。收紧旧版本建成全局可读的文件；
// 新文件由 umask(077) 保证。
- (void)secureStateDirectory {
    NSString *dir = [_statePath stringByDeletingLastPathComponent];
    [[NSFileManager defaultManager] createDirectoryAtPath:dir withIntermediateDirectories:YES
        attributes:@{ NSFilePosixPermissions: @0700 } error:nil];
    chmod(dir.fileSystemRepresentation, 0700);
    NSDirectoryEnumerator *e = [[NSFileManager defaultManager] enumeratorAtPath:dir];
    for (NSString *rel in e) {
        NSString *p = [dir stringByAppendingPathComponent:rel];
        BOOL isDir = [e.fileAttributes[NSFileType] isEqual:NSFileTypeDirectory];
        chmod(p.fileSystemRepresentation, isDir ? 0700 : 0600);
    }
}

// 必须在 _queue 上调用。
- (void)saveState {
    dispatch_assert_queue(_queue);
    NSError *err = nil;
    [[NSFileManager defaultManager]
        createDirectoryAtPath:[_statePath stringByDeletingLastPathComponent]
  withIntermediateDirectories:YES attributes:nil error:&err];
    NSData *data = [NSPropertyListSerialization
        dataWithPropertyList:@{ @"enabled": @(_enabled), @"modules": _modules.settings ?: @{} }
                      format:NSPropertyListBinaryFormat_v1_0 options:0 error:&err];
    // 原子写：写到一半被 jetsam 杀掉也不能留下残缺文件。
    if (!data || ![data writeToFile:_statePath options:NSDataWritingAtomic error:&err]) {
        os_log_error(RKLog(), "failed to save state: %{public}@", err);
    }
}

#pragma mark RKDaemonProtocol

- (void)pingWithReply:(void (^)(NSString *, uint64_t))reply {
    reply(@(RK_VERSION), (uint64_t)getpid());
}

- (void)setEnabled:(BOOL)enabled reply:(void (^)(BOOL))reply {
    dispatch_async(_queue, ^{
#ifdef RK_TEST_HANG
        // 仅测试构建：`off` 让 state 队列永久卡死，用来验证 watchdog 能把卡死变成重启。
        // 发布版绝不能定义此宏。
        if (!enabled) { os_log_fault(RKLog(), "RK_TEST_HANG: hanging"); for (;;) pause(); }
#endif
        if (self->_enabled != enabled) {
            self->_enabled = enabled;
            [self saveState];
            [self->_modules setServiceEnabled:enabled];
            [self->_modules emitEvent:@"system.service.toggled" level:@"notice"
                              payload:@{ @"enabled": @(enabled), @"by": @"app" }];
            notify_post(RK_DARWIN_CHANGED);   // 仅在真正变化时通知
            os_log(RKLog(), "enabled -> %d", enabled);
        }
        reply(self->_enabled);
    });
}

- (void)queryStatusWithReply:(void (^)(NSDictionary *))reply {
    dispatch_async(_queue, ^{
        reply(@{
            RKStatusEnabled     : @(self->_enabled),
            RKStatusPID         : @(getpid()),
            RKStatusUptime      : @([[NSDate date] timeIntervalSinceDate:self->_startedAt]),
            RKStatusVersion     : @(RK_VERSION),
            RKStatusFootprintKB : @(RKFootprintBytes() / 1024),
            RKStatusWatchdogOK  : @(RKWatchdogHealthy()),
            RKStatusKeepAliveOK : @(RKKeepAliveActive()),
            RKStatusModules     : self->_modules.moduleIdentifiers,
        });
    });
}

- (void)queryMetricsWithReply:(void (^)(NSDictionary *))reply {
    dispatch_async(_queue, ^{ reply(RKSampleMetrics()); });
}

- (void)invokeModule:(NSString *)module
             command:(NSString *)command
           arguments:(NSDictionary *)arguments
               reply:(void (^)(NSDictionary *, NSError *))reply {
    dispatch_async(_queue, ^{
        [self->_modules invokeModule:module command:command arguments:arguments reply:reply];
    });
}

@end
