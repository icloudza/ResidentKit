// RKPageViewController.h — 所有页面的基类：分组背景上纵向滚动的卡片栈，iPad 上限制可读宽度，
// 并避让键盘。子类往 `contentStack` 里加视图。

#import <UIKit/UIKit.h>

NS_ASSUME_NONNULL_BEGIN

@interface RKPageViewController : UIViewController
@property (nonatomic, readonly) UIScrollView *scrollView;
@property (nonatomic, readonly) UIStackView *contentStack;
// 本页显示时隐藏导航栏（首页用），默认 NO。
@property (nonatomic) BOOL hidesNavigationBar;
@end

NS_ASSUME_NONNULL_END
