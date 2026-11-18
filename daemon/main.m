// residentd — 由 launchd 管理的常驻 daemon。
//
// 生命周期归 launchd：plist 设了 KeepAlive=true，退出或被 jetsam 杀掉都会被重新拉起。
// 不要 fork、daemonize 或自己保活——那是 launchd 的事，对着干只会造成重启循环。
//
// Dopamine 下 launchdhook 会把 $JBROOT/Library/LaunchDaemons 中所有 plist 注入
// launchd 的启动集，每次重新越狱（userspace reboot）后自动回来，无需额外处理。
//
// 结构
//   main.m          启动：jetsam、XPC listener、watchdog、run loop
//   RKService       导出的 XPC 对象、持久化状态
//   RKModuleHost    加载功能模块（Modules/*.m，见 RKModule.h）
//   RKWatchdog      卡死 → abort → 重新拉起
//   RKMetrics       按需采样内存 / CPU
//   RKSystem        越狱路径、jetsam、launchd 检查

#import <Foundation/Foundation.h>
#import <os/log.h>
#import <sys/stat.h>
#import "../common/RKProtocol.h"
#import "../common/RKXPCCompat.h"
#import "RKService.h"
#import "RKSystem.h"
#import "RKWatchdog.h"

// iOS 私有 API，但 iOS 6 起 NSXPCConnection 就有，Apple 自家 daemon 用它校验客户端。
// Dopamine 下 ldid 签入的 entitlement 有效（二进制在 spawn 时进入 trust cache）。
@interface NSXPCConnection (RKPrivate)
- (id)valueForEntitlement:(NSString *)entitlement;
@end

@interface RKListenerDelegate : NSObject <NSXPCListenerDelegate>
@property (strong) RKService *service;
@end

@implementation RKListenerDelegate

- (BOOL)listener:(NSXPCListener *)listener shouldAcceptNewConnection:(NSXPCConnection *)conn {
    // 以 root 运行：只服务带有本 entitlement 签名的客户端。
    id granted = [conn valueForEntitlement:@RK_CLIENT_ENTITLEMENT];
    if (![granted isKindOfClass:[NSNumber class]] || ![granted boolValue]) {
        os_log_error(OS_LOG_DEFAULT, "residentd: rejected XPC client pid=%d (missing %{public}s)",
                     conn.processIdentifier, RK_CLIENT_ENTITLEMENT);
        return NO;
    }
    conn.exportedInterface = RKDaemonInterface();
    conn.exportedObject = self.service;
    [conn resume];
    return YES;
}

@end

int main(int argc, char *argv[]) {
    @autoreleasepool {
        // 创建的所有文件（state、队列——里面有服务端 token）仅 root 可访问。
        // 必须在创建任何文件之前设置。
        umask(077);

        // 第一件事：离开 BACKGROUND jetsam band。
        RKRaiseJetsamBand();

#ifdef RK_TEST_CRASH
        // 仅用于验证 OTA 自动回滚（RK_EXTRA_CFLAGS=-DRK_TEST_CRASH）：启动 5 秒后崩溃。
        dispatch_after(dispatch_time(DISPATCH_TIME_NOW, 5 * NSEC_PER_SEC), dispatch_get_main_queue(), ^{ abort(); });
#endif

        RKService *service = [RKService new];

        // listener.delegate 是 weak；下面的 run loop 永不返回，`delegate` 会一直存活。
        RKListenerDelegate *delegate = [RKListenerDelegate new];
        delegate.service = service;
        NSXPCListener *listener = RKListenerForMachService(@(RK_MACH_SERVICE));
        listener.delegate = delegate;
        [listener resume];

        // launchctl bootout / 卸载 / 升级会发 SIGTERM：记为正常退出再结束。
        signal(SIGTERM, SIG_IGN);
        static dispatch_source_t term;
        term = dispatch_source_create(DISPATCH_SOURCE_TYPE_SIGNAL, SIGTERM, 0, dispatch_get_main_queue());
        dispatch_source_set_event_handler(term, ^{
            RKRunRecordMarkExit("clean");
            exit(0);
        });
        dispatch_resume(term);

        [service start];
        RKWatchdogStart(service.queue);
        (void)RKKeepAliveActive();   // 趁启动上下文还新鲜时判定

        os_log(OS_LOG_DEFAULT, "residentd %{public}s up, pid=%d", RK_VERSION, getpid());

        // 主线程永久挂起，不空转；存活由 launchd 看管。
        [[NSRunLoop currentRunLoop] run];
    }
    return 0;
}
