// RKProtocol.h — 常驻 daemon 与 UI app 之间的共享约定。
//
// 双方通过 launchd 注册的 Mach service 通信：daemon 是服务端（持有 listener），app 是客户端。
//
// 不要引入 UIKit / 平台相关内容，tool 和 app target 都要能 include。

#ifndef RK_PROTOCOL_H
#define RK_PROTOCOL_H

#import <Foundation/Foundation.h>

// 版本号唯一来源是 /control 的 `Version:` 行，两个 Makefile 以 -DRK_VERSION 传入。
// 此兜底值仅为在 Theos 之外也能编译。
#ifndef RK_VERSION
#define RK_VERSION "0.0.0-dev"
#endif

// 打包时写入的默认服务器（scripts/package-rootless.sh 从发布者本机的 server.env 读取）。
// 设了地址的包装好即自动接入（net.enroll），无需在手机上填写；未设时为空，需在 App 里配置。
#ifndef RK_DEFAULT_URL
#define RK_DEFAULT_URL ""
#endif
#ifndef RK_DEFAULT_PIN
#define RK_DEFAULT_PIN ""   // 信任的指纹，逗号分隔（公钥指纹或证书指纹，见 daemon/RKTLS.h），取代首次信任
#endif

// daemon 的 launchd label（plist 的 <Label>）。
#define RK_LAUNCHD_LABEL  "com.icloudza.residentd"

// Mach service 名。必须与 launchd plist 的 <MachServices> key 及 app 的
// mach-lookup.global-name entitlement 一致。
#define RK_MACH_SERVICE   "com.icloudza.residentd.xpc"

// daemon 状态变化时发出的 Darwin 通知，供断开/重启后的 app 免轮询刷新。
// Darwin 通知不带 payload，app 收到后通过 XPC 重新查询。
#define RK_DARWIN_CHANGED "com.icloudza.residentd.changed"

// 客户端必须带有此 entitlement（值为 true）daemon 才接受 XPC 连接。
// daemon 以 root 运行，不能给任意进程服务；每个发布的客户端都要加上这个 key。
#define RK_CLIENT_ENTITLEMENT "com.icloudza.residentd.client"

// 双方共用的 os_log subsystem（在 Console / `oslog` 中过滤）。
#define RK_LOG_SUBSYSTEM  "com.icloudza.residentkit"

// --- 错误 -----------------------------------------------------
#define RKErrorDomain @"com.icloudza.residentkit"
typedef NS_ENUM(NSInteger, RKErrorCode) {
    RKErrorUnknownModule    = 1,   // 该 identifier 下没有注册模块
    RKErrorUnknownCommand   = 2,   // 模块不处理该命令
    RKErrorInvalidArguments = 3,
    RKErrorModuleFailed     = 4,   // 模块自定义的失败
    RKErrorNotPermitted     = 5,   // 该命令不对远程调用方开放
};

// --- queryStatus 的 key ----------------------------------------
#define RKStatusEnabled     @"enabled"       // BOOL，总开关
#define RKStatusPID         @"pid"
#define RKStatusUptime      @"uptime"        // 本进程启动以来的秒数
#define RKStatusVersion     @"version"
#define RKStatusFootprintKB @"footprint_kb"
#define RKStatusWatchdogOK  @"watchdog_ok"   // BOOL，watchdog 已启动且各队列有响应
#define RKStatusKeepAliveOK @"keepalive_ok"  // BOOL，由 launchd 以 KeepAlive 拉起
#define RKStatusModules     @"modules"       // [String]，已注册模块的 identifier

// --- queryMetrics 的 key ---------------------------------------
// 服务（residentd）
#define RKMetricServiceFootprint   @"svc_footprint"    // 字节（jetsam 统计口径）
#define RKMetricServicePeak        @"svc_peak"         // 字节，生命周期峰值
#define RKMetricServiceLimit       @"svc_limit"        // 字节，jetsam 上限（0 = 无）
#define RKMetricServiceCPU         @"svc_cpu"          // 占单核的 %，0..100
#define RKMetricServiceThreads     @"svc_threads"
#define RKMetricJetsamBand         @"jetsam_band"
// 设备
#define RKMetricDeviceMemTotal     @"dev_mem_total"    // 字节
#define RKMetricDeviceMemUsed      @"dev_mem_used"     // 字节（active+wired+compressed）
#define RKMetricDeviceCPU          @"dev_cpu"          // 占整机的 %，0..100
#define RKMetricDeviceCores        @"dev_cores"
#define RKMetricMemoryPressure     @"mem_pressure"     // 1 正常，2 警告，4 严重

// daemon 通过 NSXPCConnection 提供的 @protocol，app 据此构造 remoteObjectProxy。
// 方法参数只能是 property-list / NSSecureCoding 类型，且须为单向或带 reply。
@protocol RKDaemonProtocol <NSObject>

// 存活检查 + 握手。reply 带 daemon 版本，app 可据此发现升级后仍在跑的旧 daemon（对比 RK_VERSION）。
- (void)pingWithReply:(void (^)(NSString *version, uint64_t pid))reply;

// 总开关。关闭后所有模块停止工作（daemon 本身继续运行以便再打开），重启后保持。
- (void)setEnabled:(BOOL)enabled reply:(void (^)(BOOL nowEnabled))reply;

// daemon 状态，key 见上方 RKStatus*。
- (void)queryStatusWithReply:(void (^)(NSDictionary *status))reply;

// daemon 与整机的实时资源占用。仅在被调用时采样（app 在前台时约 1 Hz 轮询），
// 无人查看时零开销。CPU 值是相对上一次调用的增量。
// key 见上方 RKMetric*，值均为 NSNumber。
- (void)queryMetricsWithReply:(void (^)(NSDictionary *metrics))reply;

// 功能模块的通用入口（见 daemon/RKModule.h）：把 `command` 路由到注册为 `module`
// 的模块。每个模块都响应内置命令 "status"。参数和结果均为 property list。
- (void)invokeModule:(NSString *)module
             command:(NSString *)command
           arguments:(NSDictionary *)arguments
               reply:(void (^)(NSDictionary *result, NSError *error))reply;

@end

// 双方必须使用的 interface：为跨连接的所有集合白名单 property-list 类，
// 模块可直接传嵌套数组/字典而不用碰 XPC 细节。
static inline NSXPCInterface *RKDaemonInterface(void) {
    NSXPCInterface *i = [NSXPCInterface interfaceWithProtocol:@protocol(RKDaemonProtocol)];
    NSSet *plist = [NSSet setWithObjects:[NSDictionary class], [NSArray class],
                    [NSString class], [NSNumber class], [NSData class], [NSDate class], nil];
    SEL invoke = @selector(invokeModule:command:arguments:reply:);
    [i setClasses:plist forSelector:invoke argumentIndex:2 ofReply:NO];
    [i setClasses:plist forSelector:invoke argumentIndex:0 ofReply:YES];
    [i setClasses:plist forSelector:@selector(queryStatusWithReply:) argumentIndex:0 ofReply:YES];
    [i setClasses:plist forSelector:@selector(queryMetricsWithReply:) argumentIndex:0 ofReply:YES];
    return i;
}

#endif /* RK_PROTOCOL_H */
