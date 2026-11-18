#import "RKAppDelegate.h"
#import "UI/RKHomeViewController.h"
#import "UI/RKWidgets.h"
#import "Core/RKEnrollLink.h"
#ifdef RK_DEBUG_SNAPSHOT
#import "UI/RKMonitorViewController.h"
#import "Core/RKFeature.h"
#endif
#import <notify.h>

@implementation RKAppDelegate

- (BOOL)application:(UIApplication *)application
        didFinishLaunchingWithOptions:(NSDictionary *)launchOptions {
    UINavigationBarAppearance *bar = [UINavigationBarAppearance new];
    [bar configureWithOpaqueBackground];
    bar.backgroundColor = [UIColor systemGroupedBackgroundColor];
    bar.shadowColor = UIColor.clearColor;

    UINavigationController *nav =
        [[UINavigationController alloc] initWithRootViewController:[RKHomeViewController new]];
    nav.navigationBar.standardAppearance = bar;
    nav.navigationBar.scrollEdgeAppearance = bar;
    nav.navigationBar.tintColor = RKColorService();

    self.window = [[UIWindow alloc] initWithFrame:[UIScreen mainScreen].bounds];
    self.window.rootViewController = nav;
    [self.window makeKeyAndVisible];

    [self watchForUpdates];
#ifdef RK_DEBUG_SNAPSHOT
    [self installDebugSnapshot];
#endif
    return YES;
}

#pragma mark - 接入二维码

// 系统相机扫控制台的接入二维码（residentkit://enroll?…）会打开 App：弹窗确认后写入连接配置。
- (BOOL)application:(UIApplication *)app openURL:(NSURL *)url options:(NSDictionary *)options {
    RKEnrollLink *link = [RKEnrollLink linkWithString:url.absoluteString];
    if (!link) return NO;
    // 冷启动时根页面刚建好，等下一轮再弹窗。
    dispatch_async(dispatch_get_main_queue(), ^{
        UIViewController *top = self.window.rootViewController;
        while (top.presentedViewController) top = top.presentedViewController;
        [link confirmAndApplyFrom:top];
    });
    return YES;
}

#pragma mark - OTA 更新

// daemon 完成 OTA 后发通知。正在运行的仍是旧代码，需要重新载入：
//   后台：直接退出，下次打开即新版。
//   前台：显示「正在重新载入」约 1 秒后退出，由 daemon 重新拉起（自己退出后无法再启动自己）。
//   正在输入时（如填写服务器地址）：改为弹窗询问，不打断输入。
- (void)watchForUpdates {
    int token;
    __weak typeof(self) weakSelf = self;
    notify_register_dispatch("com.icloudza.residentd.updated", &token, dispatch_get_main_queue(), ^(int t) {
        [weakSelf updateInstalled];
    });
}

static BOOL RKFindEditing(UIView *v);

- (void)relaunch {
    notify_post("com.icloudza.residentkit.relaunch");
    exit(0);
}

- (void)updateInstalled {
    if (UIApplication.sharedApplication.applicationState != UIApplicationStateActive) exit(0);
    UIViewController *top = self.window.rootViewController;
    while (top.presentedViewController) top = top.presentedViewController;

    if (RKFindEditing(self.window)) {
        UIAlertController *a = [UIAlertController alertControllerWithTitle:@"已更新到新版本"
            message:@"重新打开 App 以加载新版界面。" preferredStyle:UIAlertControllerStyleAlert];
        [a addAction:[UIAlertAction actionWithTitle:@"稍后" style:UIAlertActionStyleCancel handler:nil]];
        [a addAction:[UIAlertAction actionWithTitle:@"重新打开" style:UIAlertActionStyleDefault
                                            handler:^(UIAlertAction *x) { [self relaunch]; }]];
        [top presentViewController:a animated:YES completion:nil];
        return;
    }

    // 轻提示后自动重新载入。
    UIView *hud = [UIView new];
    hud.backgroundColor = [UIColor.labelColor colorWithAlphaComponent:0.85];
    hud.layer.cornerRadius = 14;
    hud.translatesAutoresizingMaskIntoConstraints = NO;
    UILabel *l = [UILabel new];
    l.text = @"已更新，正在重新载入…";
    l.font = [UIFont systemFontOfSize:15 weight:UIFontWeightSemibold];
    l.textColor = UIColor.systemBackgroundColor;
    l.translatesAutoresizingMaskIntoConstraints = NO;
    [hud addSubview:l];
    [self.window addSubview:hud];
    [NSLayoutConstraint activateConstraints:@[
        [l.leadingAnchor constraintEqualToAnchor:hud.leadingAnchor constant:20],
        [l.trailingAnchor constraintEqualToAnchor:hud.trailingAnchor constant:-20],
        [l.topAnchor constraintEqualToAnchor:hud.topAnchor constant:14],
        [l.bottomAnchor constraintEqualToAnchor:hud.bottomAnchor constant:-14],
        [hud.centerXAnchor constraintEqualToAnchor:self.window.centerXAnchor],
        [hud.centerYAnchor constraintEqualToAnchor:self.window.centerYAnchor],
    ]];
    hud.alpha = 0;
    [UIView animateWithDuration:0.2 animations:^{ hud.alpha = 1; }];
    dispatch_after(dispatch_time(DISPATCH_TIME_NOW, (int64_t)(1.2 * NSEC_PER_SEC)), dispatch_get_main_queue(), ^{
        [self relaunch];
    });
}

// 是否有文本输入框正在编辑。
static BOOL RKFindEditing(UIView *v) {
    if (([v isKindOfClass:UITextField.class] || [v isKindOfClass:UITextView.class]) && v.isFirstResponder) return YES;
    for (UIView *s in v.subviews) if (RKFindEditing(s)) return YES;
    return NO;
}

#ifdef RK_DEBUG_SNAPSHOT
// 仅调试构建：`rkctl post com.icloudza.residentkit.snapshot` 把当前页完整内容
// （标记为 "rk.content" 的视图，含滚出屏幕的部分）渲染到 /var/tmp/rk-snapshot.png，
// 无需开发者磁盘镜像即可通过 SSH 检查 UI。调试构建以 no-sandbox 签名（见 README），故 /var/tmp 可写。
static UIView *RKFindContent(UIView *v) {
    if ([v.accessibilityIdentifier isEqualToString:@"rk.content"]) return v;
    for (UIView *s in v.subviews) { UIView *f = RKFindContent(s); if (f) return f; }
    return nil;
}

- (void)installDebugSnapshot {
    // 截图用的导航钩子：open-monitor、open-feature（每次打开下一个可选功能项）、pop。
    int nav1, nav2, nav3;
    UINavigationController *navc = (UINavigationController *)self.window.rootViewController;
    notify_register_dispatch("com.icloudza.residentkit.open-monitor", &nav1, dispatch_get_main_queue(), ^(int t) {
        [navc pushViewController:[RKMonitorViewController new] animated:NO];
    });
    __block NSUInteger next = 0;
    notify_register_dispatch("com.icloudza.residentkit.open-feature", &nav2, dispatch_get_main_queue(), ^(int t) {
        NSArray *sel = [RKMakeRegisteredFeatures([RKClient shared]) filteredArrayUsingPredicate:
                        [NSPredicate predicateWithFormat:@"selectable == YES"]];
        if (!sel.count) return;
        [navc pushViewController:[sel[next++ % sel.count] makeViewController] animated:NO];
    });
    notify_register_dispatch("com.icloudza.residentkit.pop", &nav3, dispatch_get_main_queue(), ^(int t) {
        [navc popToRootViewControllerAnimated:NO];
    });

    int token;
    notify_register_dispatch("com.icloudza.residentkit.snapshot", &token,
                             dispatch_get_main_queue(), ^(int t) {
        UINavigationController *nav = (UINavigationController *)self.window.rootViewController;
        UIViewController *top = nav.topViewController;
        UIView *content = RKFindContent(top.view) ?: top.view;
        CGFloat pad = 16;
        CGFloat barH = nav.navigationBarHidden ? 0 : 44;
        CGSize full = CGSizeMake(content.bounds.size.width + 2 * pad,
                                 content.bounds.size.height + 2 * pad + barH);
        UIImage *img = [[[UIGraphicsImageRenderer alloc] initWithSize:full]
            imageWithActions:^(UIGraphicsImageRendererContext *c) {
            [top.view.backgroundColor setFill];
            UIRectFill(CGRectMake(0, 0, full.width, full.height));
            if (barH) {
                NSString *title = top.title ?: @"";
                NSDictionary *a = @{ NSFontAttributeName: [UIFont systemFontOfSize:17 weight:UIFontWeightSemibold],
                                     NSForegroundColorAttributeName: UIColor.labelColor };
                CGSize ts = [title sizeWithAttributes:a];
                [title drawAtPoint:CGPointMake((full.width - ts.width) / 2, (barH - ts.height) / 2) withAttributes:a];
                [@"‹ 返回" drawAtPoint:CGPointMake(pad, (barH - ts.height) / 2)
                       withAttributes:@{ NSFontAttributeName: [UIFont systemFontOfSize:17],
                                         NSForegroundColorAttributeName: RKColorService() }];
            }
            CGContextTranslateCTM(c.CGContext, pad, pad + barH);
            [content.layer renderInContext:c.CGContext];
        }];
        NSError *err = nil;
        BOOL ok = [UIImagePNGRepresentation(img) writeToFile:@"/var/tmp/rk-snapshot.png"
                                                     options:NSDataWritingAtomic error:&err];
        NSLog(@"[ResidentKit] snapshot %@ %@x%@ ok=%d %@", NSStringFromClass(top.class),
              @(full.width), @(full.height), ok, err ?: @"");
    });
}
#endif

@end
