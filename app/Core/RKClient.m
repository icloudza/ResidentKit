// RKClient.m — 说明见头文件。

#import "RKClient.h"
#import "../../common/RKProtocol.h"
#import "../../common/RKXPCCompat.h"
#import <UIKit/UIKit.h>
#import <notify.h>

NSNotificationName const RKClientDidUpdateNotification = @"RKClientDidUpdateNotification";
NSNotificationName const RKClientDaemonDidChangeNotification = @"RKClientDaemonDidChangeNotification";

@implementation RKClient {
    NSXPCConnection *_connection;
    BOOL _versionChecked, _versionChecking;
    BOOL _metricsInFlight;
    int _notifyToken;
}

+ (instancetype)shared {
    static RKClient *client;
    static dispatch_once_t once;
    dispatch_once(&once, ^{ client = [RKClient new]; });
    return client;
}

- (instancetype)init {
    if ((self = [super init])) {
        // daemon 每次状态真正变化时都会发此通知。libnotify 在进程生命周期内
        // 持有该 block，与单例生命周期一致。
        __weak typeof(self) weakSelf = self;
        notify_register_dispatch(RK_DARWIN_CHANGED, &_notifyToken, dispatch_get_main_queue(), ^(int t) {
            [weakSelf daemonDidChange];
        });
        [[NSNotificationCenter defaultCenter] addObserver:self selector:@selector(daemonDidChange)
            name:UIApplicationWillEnterForegroundNotification object:nil];
    }
    return self;
}

- (BOOL)serviceEnabled { return [_status[RKStatusEnabled] boolValue]; }

- (void)daemonDidChange {
    [self refreshStatus];
    [[NSNotificationCenter defaultCenter] postNotificationName:RKClientDaemonDidChangeNotification object:self];
}

#pragma mark - 连接
- (NSXPCConnection *)connection {
    NSAssert([NSThread isMainThread], @"RKClient is main-thread only");
    if (_connection) return _connection;

    // privileged 选项在 system domain 查找服务名，LaunchDaemon 的 MachServices 注册在那里。
    NSXPCConnection *conn = RKConnectionToMachService(@(RK_MACH_SERVICE), NSXPCConnectionPrivileged);
    conn.remoteObjectInterface = RKDaemonInterface();

    __weak typeof(self) weakSelf = self;
    __weak NSXPCConnection *weakConn = conn;
    // Invalidated：该连接对象永久失效。若仍是当前连接则丢弃，下次调用重建。
    conn.invalidationHandler = ^{
        dispatch_async(dispatch_get_main_queue(), ^{
            typeof(self) self_ = weakSelf;
            if (self_ && self_->_connection == weakConn) {
                self_->_connection = nil;
                self_->_versionChecked = NO;
                [self_ setConnected:NO error:@"连接已断开"];
            }
        });
    };
    // Interrupted：daemon 退出（jetsam、崩溃、watchdog、升级）。launchd 会重新拉起，
    // 本连接在下一条消息时自动重连。
    conn.interruptionHandler = ^{
        dispatch_async(dispatch_get_main_queue(), ^{
            typeof(self) self_ = weakSelf;
            if (!self_) return;
            self_->_versionChecked = NO;
            [self_ setConnected:NO error:@"服务重启中…"];
            [self_ daemonDidChange];
        });
    };
    [conn resume];
    _connection = conn;
    return conn;
}

// 传输出错时将状态标为断开的 proxy；`onError` 在主线程执行。
- (id<RKDaemonProtocol>)daemon:(nullable void (^)(NSError *))onError {
    __weak typeof(self) weakSelf = self;
    return [self.connection remoteObjectProxyWithErrorHandler:^(NSError *e) {
        dispatch_async(dispatch_get_main_queue(), ^{
            [weakSelf setConnected:NO error:e.localizedDescription];
            if (onError) onError(e);
        });
    }];
}

- (void)setConnected:(BOOL)connected error:(nullable NSString *)error {
    if (_connected == connected && (connected || [_lastError isEqualToString:error])) return;
    _connected = connected;
    _lastError = connected ? nil : error;
    [[NSNotificationCenter defaultCenter] postNotificationName:RKClientDidUpdateNotification object:self];
}

// 升级包后旧 daemon 可能仍在运行（如 postinst 重载失败）。每条连接检查一次。
// 成功拿到版本后才算检查过；失败（如服务正在重启）时下次刷新再试。
- (void)checkVersionIfNeeded {
    if (_versionChecked || _versionChecking) return;
    _versionChecking = YES;
    __weak typeof(self) weakSelf = self;
    id<RKDaemonProtocol> d = [self daemon:^(NSError *e) {
        typeof(self) self_ = weakSelf;
        if (self_) self_->_versionChecking = NO;
    }];
    [d pingWithReply:^(NSString *version, uint64_t pid) {
        dispatch_async(dispatch_get_main_queue(), ^{
            typeof(self) self_ = weakSelf;
            if (!self_) return;
            self_->_versionChecking = NO;
            self_->_versionChecked = YES;
            if ([version isEqualToString:@(RK_VERSION)]) { self_->_versionWarning = nil; return; }
            // 磁盘上的 App 已是新版本（OTA 或 dpkg 升级后本进程未重启）：重新打开即可，无需重装。
            // NSBundle.infoDictionary 有缓存，直接读文件。
            NSString *plist = [NSBundle.mainBundle.bundlePath stringByAppendingPathComponent:@"Info.plist"];
            NSString *onDisk = [NSDictionary dictionaryWithContentsOfFile:plist][@"CFBundleShortVersionString"];
            self_->_versionWarning = [onDisk isEqualToString:version]
                ? [NSString stringWithFormat:@"App 已更新到 %@，从后台划掉后重新打开即可", version]
                : [NSString stringWithFormat:@"服务版本 %@ 与 App %s 不一致，请重新安装", version, RK_VERSION];
        });
    }];
}

#pragma mark - 调用
- (void)refreshStatus {
    [self checkVersionIfNeeded];
    __weak typeof(self) weakSelf = self;
    [[self daemon:nil] queryStatusWithReply:^(NSDictionary *status) {
        dispatch_async(dispatch_get_main_queue(), ^{
            typeof(self) self_ = weakSelf;
            if (!self_ || !status) return;
            self_->_status = status;
            self_->_connected = YES;
            self_->_lastError = nil;
            [[NSNotificationCenter defaultCenter] postNotificationName:RKClientDidUpdateNotification object:self_];
        });
    }];
}

- (void)fetchMetrics:(void (^)(NSDictionary *))completion {
    if (_metricsInFlight) return;
    _metricsInFlight = YES;
    __weak typeof(self) weakSelf = self;
    id<RKDaemonProtocol> d = [self daemon:^(NSError *e) {
        typeof(self) self_ = weakSelf;
        if (self_) self_->_metricsInFlight = NO;
        completion(nil);
    }];
    [d queryMetricsWithReply:^(NSDictionary *m) {
        dispatch_async(dispatch_get_main_queue(), ^{
            typeof(self) self_ = weakSelf;
            if (self_) self_->_metricsInFlight = NO;
            completion(m);
        });
    }];
}

- (void)setServiceEnabled:(BOOL)enabled completion:(void (^)(BOOL, BOOL))completion {
    __weak typeof(self) weakSelf = self;
    BOOL before = self.serviceEnabled;
    id<RKDaemonProtocol> d = [self daemon:^(NSError *e) {
        if (completion) completion(NO, before);
    }];
    // 状态真正变化时 daemon 还会发 RK_DARWIN_CHANGED。
    [d setEnabled:enabled reply:^(BOOL now) {
        dispatch_async(dispatch_get_main_queue(), ^{
            [weakSelf refreshStatus];
            if (completion) completion(YES, now);
        });
    }];
}

- (void)invokeModule:(NSString *)module
             command:(NSString *)command
           arguments:(NSDictionary *)arguments
          completion:(void (^)(NSDictionary *, NSError *))completion {
    id<RKDaemonProtocol> d = [self daemon:^(NSError *e) {
        if (completion) completion(nil, e);
    }];
    [d invokeModule:module command:command arguments:arguments ?: @{}
              reply:^(NSDictionary *result, NSError *error) {
        dispatch_async(dispatch_get_main_queue(), ^{
            if (completion) completion(error ? nil : (result ?: @{}), error);
        });
    }];
}

@end
