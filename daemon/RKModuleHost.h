// RKModuleHost.h — 实例化已注册模块并驱动其生命周期。
// daemon 内部使用，模块只能看到 RKModuleContext。

#import "RKModule.h"

NS_ASSUME_NONNULL_BEGIN

@interface RKModuleHost : NSObject

// `settings` 是持久化的 { moduleId: { key: value } }；模块改设置时在 `queue` 上调 `persist`。
// `dataRoot` 下每个模块一个子目录（RKModuleContext.dataDirectory）。
- (instancetype)initWithQueue:(dispatch_queue_t)queue
                     settings:(nullable NSDictionary *)settings
                     dataRoot:(NSString *)dataRoot
                      persist:(void (^)(void))persist;

@property (nonatomic, readonly) NSArray<NSString *> *moduleIdentifiers;
@property (nonatomic, readonly) NSDictionary *settings;   // 用于保存的快照

- (BOOL)module:(NSString *)module allowsRemoteCommand:(NSString *)command;
// 经 net.link 发送事件；须在 queue 上调用。
- (void)emitEvent:(NSString *)type level:(NSString *)level payload:(nullable NSDictionary *)payload;
@property (nonatomic, readonly) NSDictionary<NSString *, NSArray<NSString *> *> *remoteCommandMap;
@property (nonatomic, readonly) NSDictionary<NSString *, NSDictionary *> *commandSchemaMap;

// 以下方法都必须在 `queue` 上调用。
- (void)setServiceEnabled:(BOOL)enabled;
- (void)invokeModule:(NSString *)module
             command:(NSString *)command
           arguments:(nullable NSDictionary *)arguments
               reply:(RKModuleReply)reply;

@end

NS_ASSUME_NONNULL_END
