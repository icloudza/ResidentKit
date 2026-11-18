// RKPageViewController.m — 说明见头文件。

#import "RKPageViewController.h"

@implementation RKPageViewController

- (void)viewDidLoad {
    [super viewDidLoad];
    self.view.backgroundColor = [UIColor systemGroupedBackgroundColor];

    _scrollView = [UIScrollView new];
    _scrollView.alwaysBounceVertical = YES;
    _scrollView.keyboardDismissMode = UIScrollViewKeyboardDismissModeInteractive;
    _scrollView.translatesAutoresizingMaskIntoConstraints = NO;
    [self.view addSubview:_scrollView];

    _contentStack = [UIStackView new];
    _contentStack.axis = UILayoutConstraintAxisVertical;
    _contentStack.spacing = 16;
    _contentStack.accessibilityIdentifier = @"rk.content";   // 调试截图用
    _contentStack.translatesAutoresizingMaskIntoConstraints = NO;
    [_scrollView addSubview:_contentStack];

    UILayoutGuide *safe = self.view.safeAreaLayoutGuide;
    NSLayoutConstraint *fill = [_contentStack.widthAnchor constraintEqualToAnchor:safe.widthAnchor constant:-32];
    fill.priority = UILayoutPriorityDefaultHigh;   // iPad 上让位于 640 pt 上限
    [NSLayoutConstraint activateConstraints:@[
        [_scrollView.topAnchor constraintEqualToAnchor:self.view.topAnchor],
        [_scrollView.leadingAnchor constraintEqualToAnchor:self.view.leadingAnchor],
        [_scrollView.trailingAnchor constraintEqualToAnchor:self.view.trailingAnchor],
        [_contentStack.topAnchor constraintEqualToAnchor:_scrollView.contentLayoutGuide.topAnchor constant:8],
        [_contentStack.bottomAnchor constraintEqualToAnchor:_scrollView.contentLayoutGuide.bottomAnchor constant:-24],
        [_contentStack.centerXAnchor constraintEqualToAnchor:safe.centerXAnchor],
        [_contentStack.widthAnchor constraintLessThanOrEqualToConstant:640],
        // 硬上限：任何内容都不能把页面撑出屏幕（两侧始终留 16 pt）
        [_contentStack.widthAnchor constraintLessThanOrEqualToAnchor:safe.widthAnchor constant:-32],
        fill,
    ]];
    // 滚动视图始终铺满到屏幕底部；键盘只通过底部留白避让（见下）。
    [_scrollView.bottomAnchor constraintEqualToAnchor:self.view.bottomAnchor].active = YES;
}

- (void)viewWillAppear:(BOOL)animated {
    [super viewWillAppear:animated];
    [self.navigationController setNavigationBarHidden:self.hidesNavigationBar animated:animated];
    // 只在页面可见时跟踪键盘：不可见的页面收不到键盘收起，留白会残留。
    NSNotificationCenter *nc = [NSNotificationCenter defaultCenter];
    [nc addObserver:self selector:@selector(keyboardWillChange:)
               name:UIKeyboardWillChangeFrameNotification object:nil];
    [nc addObserver:self selector:@selector(keyboardWillHide:)
               name:UIKeyboardWillHideNotification object:nil];
}

- (void)viewWillDisappear:(BOOL)animated {
    [super viewWillDisappear:animated];
    [self.view endEditing:YES];
    NSNotificationCenter *nc = [NSNotificationCenter defaultCenter];
    [nc removeObserver:self name:UIKeyboardWillChangeFrameNotification object:nil];
    [nc removeObserver:self name:UIKeyboardWillHideNotification object:nil];
    [self setKeyboardInset:0];
}

- (void)keyboardWillChange:(NSNotification *)n {
    CGRect end = [n.userInfo[UIKeyboardFrameEndUserInfoKey] CGRectValue];
    CGRect inView = [self.view convertRect:end fromView:nil];
    // 键盘与本视图重叠的高度，再扣掉已由安全区处理的部分。
    CGFloat overlap = MAX(0, CGRectGetMaxY(self.view.bounds) - CGRectGetMinY(inView));
    [self setKeyboardInset:MAX(0, overlap - self.view.safeAreaInsets.bottom)];
}

- (void)keyboardWillHide:(NSNotification *)n {
    [self setKeyboardInset:0];
}

- (void)setKeyboardInset:(CGFloat)inset {
    UIEdgeInsets c = _scrollView.contentInset, i = _scrollView.verticalScrollIndicatorInsets;
    c.bottom = inset;
    i.bottom = inset;
    _scrollView.contentInset = c;
    _scrollView.verticalScrollIndicatorInsets = i;
}

@end
