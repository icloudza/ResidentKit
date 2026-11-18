// RKXPCCompat.h — iOS 上 Mach service 版 XPC 初始化方法的兼容封装。
//
// -[NSXPCListener initWithMachServiceName:] 和
// -[NSXPCConnection initWithMachServiceName:options:] 在 iOS 上可用（Apple 自家
// daemon 就在用），但公开 SDK 标成 API_UNAVAILABLE，直接调用在 Xcode SDK 下编不过。
// 这里用带类型的 objc_msgSend 发同样的 selector，Xcode SDK 和 Theos 补丁 SDK 都能编。

#ifndef RK_XPC_COMPAT_H
#define RK_XPC_COMPAT_H

#import <Foundation/Foundation.h>
#import <objc/message.h>

static inline NSXPCListener *RKListenerForMachService(NSString *name) {
    id obj = [NSXPCListener alloc];
    return ((id (*)(id, SEL, NSString *))objc_msgSend)(
        obj, NSSelectorFromString(@"initWithMachServiceName:"), name);
}

static inline NSXPCConnection *RKConnectionToMachService(NSString *name,
                                                         NSXPCConnectionOptions options) {
    id obj = [NSXPCConnection alloc];
    return ((id (*)(id, SEL, NSString *, NSXPCConnectionOptions))objc_msgSend)(
        obj, NSSelectorFromString(@"initWithMachServiceName:options:"), name, options);
}

#endif /* RK_XPC_COMPAT_H */
