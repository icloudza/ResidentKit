// RKFeatureViews.h — RKFeature 在首页上的视图。

#import <UIKit/UIKit.h>
#import "../Core/RKFeature.h"

NS_ASSUME_NONNULL_BEGIN

// 基类：-update 时重新读取 feature；点击时发 UIControlEventTouchUpInside
// （仅当 feature 可点击）。
@interface RKFeatureView : UIControl
@property (nonatomic, readonly) RKFeature *feature;
- (instancetype)initWithFeature:(RKFeature *)feature;
- (void)update;
@end

@interface RKFeatureTileView : RKFeatureView   // 网格单元
@end

@interface RKFeatureRowView : RKFeatureView    // 列表行
@end

NS_ASSUME_NONNULL_END
