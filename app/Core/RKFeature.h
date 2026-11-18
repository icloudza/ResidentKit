// RKFeature.h — 首页的扩展点。
//
// 一个功能项即「功能」区的一个条目：网格中的磁贴或列表中的一行，可带开关、可打开独立页面。
// 新增功能项：在 app/Features/ 下建文件，继承 RKFeature，并在 @implementation 中写
// RK_REGISTER_FEATURE()。构建按通配符自动收录，首页自动渲染，无需改动其他代码。
//
// 典型写法：一个功能项对应一个 daemon 模块（daemon/Modules），缓存模块状态，
// 在 -reload 中经 self.client 刷新，缓存变化时调用 -setNeedsUpdate。
//
// 线程：与其他 UI 一样仅限主线程。

#import <UIKit/UIKit.h>
#import "RKClient.h"

NS_ASSUME_NONNULL_BEGIN

typedef NS_ENUM(NSInteger, RKFeatureStyle) {
    RKFeatureStyleTile,   // 两列网格：快捷入口、页面入口
    RKFeatureStyleRow,    // 列表：类设置项，通常带开关
};

@class RKFeature;

// 由首页实现。
@protocol RKFeatureHost <NSObject>
- (void)featureNeedsUpdate:(RKFeature *)feature;
@end

@interface RKFeature : NSObject

// 同一 style 内越小越靠前，默认 100。
@property (class, nonatomic, readonly) NSInteger order;

@property (nonatomic, readonly) RKClient *client;
@property (nonatomic, weak, nullable) id<RKFeatureHost> host;

// --- 展示（子类重写） ---------------------------------------------------------
@property (nonatomic, readonly) RKFeatureStyle style;          // 默认 Tile
@property (nonatomic, readonly) NSString *title;
@property (nonatomic, readonly) NSString *symbolName;          // SF Symbol
@property (nonatomic, readonly, nullable) NSString *subtitle;
// 当前是否可用，默认为已连接 daemon。不可用时置灰且不可交互。
@property (nonatomic, readonly, getter=isAvailable) BOOL available;

// --- 开关（仅 Row） -----------------------------------------------------------
@property (nonatomic, readonly) BOOL showsSwitch;              // 默认 NO
@property (nonatomic, readonly, getter=isOn) BOOL on;
// 用户拨动开关：执行后调用 -setNeedsUpdate（失败也要调，让开关回弹到 -isOn）。
- (void)switchDidChange:(BOOL)on;

// --- 点击 -------------------------------------------------------------------
// YES 表示可点击，点击后 push -makeViewController。默认 NO。
@property (nonatomic, readonly, getter=isSelectable) BOOL selectable;
- (nullable UIViewController *)makeViewController;

// --- 状态 -------------------------------------------------------------------
// 首页出现及 daemon 报告变更时调用：拉取后调用 -setNeedsUpdate。默认空实现。
- (void)reload;
- (void)setNeedsUpdate;

- (instancetype)initWithClient:(RKClient *)client NS_DESIGNATED_INITIALIZER;
- (instancetype)init NS_UNAVAILABLE;

@end

void RKRegisterFeature(Class featureClass);
// 返回实例化后的功能项，按 (style, +order, title) 排序。
NSArray<RKFeature *> *RKMakeRegisteredFeatures(RKClient *client);

#define RK_REGISTER_FEATURE() + (void)load { RKRegisterFeature(self); }

NS_ASSUME_NONNULL_END
