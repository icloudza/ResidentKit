// RKService.h — residentd 通过 XPC 导出的对象。
//
// 持有持久化状态（总开关 + 模块设置）和 module host，所有状态只在 `queue` 上访问。

#import <Foundation/Foundation.h>
#import "../common/RKProtocol.h"

NS_ASSUME_NONNULL_BEGIN

@interface RKService : NSObject <RKDaemonProtocol>
@property (nonatomic, readonly) dispatch_queue_t queue;
// 持久化的总开关为开时启动模块。由 main 调用一次。
- (void)start;
@end

NS_ASSUME_NONNULL_END
