// RKSummaryCardView.h — 首页上精简的「性能监控」卡片。
// 每种资源一行：标签 · 服务数值 · 对比条 · 占整机比例。点击发 UIControlEventTouchUpInside。

#import <UIKit/UIKit.h>

NS_ASSUME_NONNULL_BEGIN

@interface RKSummaryCardView : UIControl
// 传入 queryMetrics 的回复（RKMetric* key）；nil 清空数值。
- (void)applyMetrics:(nullable NSDictionary *)metrics;
@end

NS_ASSUME_NONNULL_END
