// RKClient.h — App 与 residentd 之间唯一的连接。
//
// 所有页面和功能项都经 +shared 访问 daemon：只有一条 XPC 连接、一套重连策略、
// 一处持有 daemon 状态。App 不持有任何常驻状态，被杀、被划掉或被挂起都不影响
// daemon，客户端重新连上即可。
//
// 线程：仅限主线程，所有回调都在主线程。

#import <Foundation/Foundation.h>

NS_ASSUME_NONNULL_BEGIN

// 连接状态或 status 字典变化，用于刷新头部类 UI。
extern NSNotificationName const RKClientDidUpdateNotification;
// daemon 发出变更（Darwin 通知）或已重启，功能项应重新查询各自模块。
extern NSNotificationName const RKClientDaemonDidChangeNotification;

@interface RKClient : NSObject

+ (instancetype)shared;

@property (nonatomic, readonly, getter=isConnected) BOOL connected;
// 最近一次 queryStatus 的回复（key 见 RKProtocol.h 的 RKStatus*）。首次成功前为 nil；
// 断开期间保留旧值。
@property (nonatomic, readonly, nullable) NSDictionary *status;
// 断开时的可读原因。
@property (nonatomic, readonly, nullable) NSString *lastError;
// 运行中的 daemon 版本与 App 不一致时非 nil。
@property (nonatomic, readonly, nullable) NSString *versionWarning;

@property (nonatomic, readonly) BOOL serviceEnabled;   // status[RKStatusEnabled]

- (void)refreshStatus;

// 失败时回调 nil。已有请求在途时直接丢弃本次调用（不回调），避免慢回复堆积。
- (void)fetchMetrics:(void (^)(NSDictionary *_Nullable metrics))completion;

// `nowEnabled` 为调用后 daemon 的实际值（失败时为原值）。
- (void)setServiceEnabled:(BOOL)enabled
               completion:(nullable void (^)(BOOL ok, BOOL nowEnabled))completion;

// 调用 daemon 模块（daemon/Modules）。result 与 error 恰有一个非 nil；
// 所有模块都支持 "status" 命令。
- (void)invokeModule:(NSString *)module
             command:(NSString *)command
           arguments:(nullable NSDictionary *)arguments
          completion:(nullable void (^)(NSDictionary *_Nullable result, NSError *_Nullable error))completion;

@end

NS_ASSUME_NONNULL_END
