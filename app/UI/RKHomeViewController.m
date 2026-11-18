// RKHomeViewController.m — 说明见头文件。
//
// 资源约定：仅在本页可见且 App 在前台时每 3 秒刷新一次（status + 一次 metrics 采样供摘要卡片）。
// 监控页自行以 1 Hz 采样；后台不做任何轮询。

#import "RKHomeViewController.h"
#import "RKMonitorViewController.h"
#import "RKSummaryCardView.h"
#import "RKFeatureViews.h"
#import "RKWidgets.h"
#import "../Core/RKClient.h"
#import "../Core/RKFeature.h"
#import "../../common/RKProtocol.h"

static const NSTimeInterval RKHomeRefreshInterval = 3.0;

@interface RKHomeViewController () <RKFeatureHost>
@end

@implementation RKHomeViewController {
    RKClient *_client;
    NSTimer *_timer;

    UIView *_statusDot;
    UILabel *_statusLabel, *_detailLabel;
    UISwitch *_enableSwitch;
    RKSummaryCardView *_summary;

    NSArray<RKFeature *> *_features;
    NSMapTable<RKFeature *, RKFeatureView *> *_featureViews;
}

- (instancetype)initWithNibName:(NSString *)nib bundle:(NSBundle *)bundle {
    if ((self = [super initWithNibName:nib bundle:bundle])) {
        _client = [RKClient shared];
        _featureViews = [NSMapTable strongToWeakObjectsMapTable];
        self.navigationItem.backButtonTitle = @"返回";
    }
    return self;
}

#pragma mark - 布局
- (void)viewDidLoad {
    [super viewDidLoad];
    self.hidesNavigationBar = YES;

    UIRefreshControl *rc = [UIRefreshControl new];
    [rc addTarget:self action:@selector(pullToRefresh:) forControlEvents:UIControlEventValueChanged];
    self.scrollView.refreshControl = rc;

    _summary = [RKSummaryCardView new];
    [_summary addTarget:self action:@selector(openMonitor) forControlEvents:UIControlEventTouchUpInside];

    UIStackView *stack = self.contentStack;
    stack.spacing = 14;
    [stack addArrangedSubview:[self buildHeader]];
    [stack addArrangedSubview:_summary];
    [self buildFeaturesInto:stack];
    [stack addArrangedSubview:[self buildFooter]];

    NSNotificationCenter *nc = [NSNotificationCenter defaultCenter];
    [nc addObserver:self selector:@selector(clientDidUpdate)
               name:RKClientDidUpdateNotification object:_client];
    [nc addObserver:self selector:@selector(reloadFeatures)
               name:RKClientDaemonDidChangeNotification object:_client];
    [nc addObserver:self selector:@selector(appWillEnterForeground)
               name:UIApplicationWillEnterForegroundNotification object:nil];
    [nc addObserver:self selector:@selector(stopTimer)
               name:UIApplicationDidEnterBackgroundNotification object:nil];

    [self clientDidUpdate];
}

- (UIView *)buildHeader {
    UIView *h = [UIView new];
    h.backgroundColor = RKColorHeader();
    h.layer.cornerRadius = 26;
    h.layer.cornerCurve = kCACornerCurveContinuous;

    UIImageView *icon = [[UIImageView alloc] initWithImage:[UIImage imageNamed:@"AppIcon60x60"]];
    icon.layer.cornerRadius = 13;
    icon.layer.cornerCurve = kCACornerCurveContinuous;
    icon.layer.masksToBounds = YES;
    [icon.widthAnchor constraintEqualToConstant:56].active = YES;
    [icon.heightAnchor constraintEqualToConstant:56].active = YES;

    UILabel *title = [UILabel new];
    title.text = @"ResidentKit";
    title.font = [UIFont systemFontOfSize:28 weight:UIFontWeightBold];

    _statusDot = [UIView new];
    _statusDot.layer.cornerRadius = 4;
    [_statusDot.widthAnchor constraintEqualToConstant:8].active = YES;
    [_statusDot.heightAnchor constraintEqualToConstant:8].active = YES;
    _statusLabel = [UILabel new];
    _statusLabel.font = [UIFont systemFontOfSize:13 weight:UIFontWeightSemibold];
    UIStackView *pillContent = [[UIStackView alloc] initWithArrangedSubviews:@[_statusDot, _statusLabel]];
    pillContent.spacing = 6;
    pillContent.alignment = UIStackViewAlignmentCenter;
    pillContent.translatesAutoresizingMaskIntoConstraints = NO;
    UIView *pill = [UIView new];
    pill.backgroundColor = [UIColor secondarySystemGroupedBackgroundColor];
    pill.layer.cornerRadius = 13;
    [pill addSubview:pillContent];
    [NSLayoutConstraint activateConstraints:@[
        [pillContent.topAnchor constraintEqualToAnchor:pill.topAnchor constant:5],
        [pillContent.bottomAnchor constraintEqualToAnchor:pill.bottomAnchor constant:-5],
        [pillContent.leadingAnchor constraintEqualToAnchor:pill.leadingAnchor constant:10],
        [pillContent.trailingAnchor constraintEqualToAnchor:pill.trailingAnchor constant:-12],
    ]];

    UIStackView *titleCol = [[UIStackView alloc] initWithArrangedSubviews:@[title, pill]];
    titleCol.axis = UILayoutConstraintAxisVertical;
    titleCol.spacing = 6;
    titleCol.alignment = UIStackViewAlignmentLeading;
    UIStackView *top = [[UIStackView alloc] initWithArrangedSubviews:@[icon, titleCol]];
    top.spacing = 14;
    top.alignment = UIStackViewAlignmentCenter;

    _detailLabel = [UILabel new];
    _detailLabel.font = [UIFont monospacedDigitSystemFontOfSize:13 weight:UIFontWeightMedium];
    _detailLabel.textColor = [UIColor secondaryLabelColor];
    _detailLabel.numberOfLines = 0;

    UILabel *switchTitle = [UILabel new];
    switchTitle.text = @"常驻服务";
    switchTitle.font = [UIFont systemFontOfSize:17 weight:UIFontWeightSemibold];
    UILabel *switchSub = [UILabel new];
    switchSub.text = @"关掉 App 也会继续运行";
    switchSub.font = [UIFont systemFontOfSize:12];
    switchSub.textColor = [UIColor secondaryLabelColor];
    UIStackView *switchText = [[UIStackView alloc] initWithArrangedSubviews:@[switchTitle, switchSub]];
    switchText.axis = UILayoutConstraintAxisVertical;
    switchText.spacing = 2;
    _enableSwitch = [UISwitch new];
    _enableSwitch.onTintColor = RKColorService();
    [_enableSwitch addTarget:self action:@selector(toggle:) forControlEvents:UIControlEventValueChanged];
    UIStackView *switchRow = [[UIStackView alloc] initWithArrangedSubviews:@[switchText, _enableSwitch]];
    switchRow.alignment = UIStackViewAlignmentCenter;

    UIStackView *col = [[UIStackView alloc] initWithArrangedSubviews:@[top, _detailLabel, RKHairline(), switchRow]];
    col.axis = UILayoutConstraintAxisVertical;
    col.spacing = 14;
    [col setCustomSpacing:10 afterView:top];
    col.translatesAutoresizingMaskIntoConstraints = NO;
    [h addSubview:col];
    [NSLayoutConstraint activateConstraints:@[
        [col.topAnchor constraintEqualToAnchor:h.topAnchor constant:20],
        [col.bottomAnchor constraintEqualToAnchor:h.bottomAnchor constant:-18],
        [col.leadingAnchor constraintEqualToAnchor:h.leadingAnchor constant:20],
        [col.trailingAnchor constraintEqualToAnchor:h.trailingAnchor constant:-20],
    ]];
    return h;
}

- (void)buildFeaturesInto:(UIStackView *)stack {
    _features = RKMakeRegisteredFeatures(_client);
    if (_features.count == 0) return;   // 无注册项时不显示空区块

    UILabel *section = [UILabel new];
    section.text = @"功能";
    section.font = [UIFont systemFontOfSize:22 weight:UIFontWeightBold];
    UIView *sectionBox = [UIView new];
    section.translatesAutoresizingMaskIntoConstraints = NO;
    [sectionBox addSubview:section];
    [NSLayoutConstraint activateConstraints:@[
        [section.topAnchor constraintEqualToAnchor:sectionBox.topAnchor constant:10],
        [section.bottomAnchor constraintEqualToAnchor:sectionBox.bottomAnchor],
        [section.leadingAnchor constraintEqualToAnchor:sectionBox.leadingAnchor constant:4],
    ]];
    [stack addArrangedSubview:sectionBox];
    [stack setCustomSpacing:10 afterView:sectionBox];

    NSMutableArray<RKFeatureView *> *tiles = [NSMutableArray array], *rows = [NSMutableArray array];
    for (RKFeature *f in _features) {
        f.host = self;
        RKFeatureView *v = f.style == RKFeatureStyleRow ? [[RKFeatureRowView alloc] initWithFeature:f]
                                                        : [[RKFeatureTileView alloc] initWithFeature:f];
        [v addTarget:self action:@selector(featureTapped:) forControlEvents:UIControlEventTouchUpInside];
        [_featureViews setObject:v forKey:f];
        [(f.style == RKFeatureStyleRow ? rows : tiles) addObject:v];
    }

    // 网格：两列等宽；奇数个时最后一块仍保持半宽。
    for (NSUInteger i = 0; i < tiles.count; i += 2) {
        UIView *right = i + 1 < tiles.count ? tiles[i + 1] : [UIView new];
        UIStackView *line = [[UIStackView alloc] initWithArrangedSubviews:@[tiles[i], right]];
        line.spacing = 12;
        line.distribution = UIStackViewDistributionFillEqually;
        [stack addArrangedSubview:line];
        [stack setCustomSpacing:12 afterView:line];
    }

    // 列表：一张圆角卡片，行间用细分隔线。
    if (rows.count) {
        UIStackView *list = [UIStackView new];
        list.axis = UILayoutConstraintAxisVertical;
        for (NSUInteger i = 0; i < rows.count; i++) {
            if (i) {
                UIView *sep = RKHairline();
                UIView *inset = [UIView new];
                sep.translatesAutoresizingMaskIntoConstraints = NO;
                [inset addSubview:sep];
                [NSLayoutConstraint activateConstraints:@[
                    [sep.leadingAnchor constraintEqualToAnchor:inset.leadingAnchor constant:58],
                    [sep.trailingAnchor constraintEqualToAnchor:inset.trailingAnchor],
                    [sep.topAnchor constraintEqualToAnchor:inset.topAnchor],
                    [sep.bottomAnchor constraintEqualToAnchor:inset.bottomAnchor],
                ]];
                [list addArrangedSubview:inset];
            }
            [list addArrangedSubview:rows[i]];
        }
        UIView *card = [UIView new];
        card.backgroundColor = [UIColor secondarySystemGroupedBackgroundColor];
        card.layer.cornerRadius = 20;
        card.layer.cornerCurve = kCACornerCurveContinuous;
        card.clipsToBounds = YES;
        list.translatesAutoresizingMaskIntoConstraints = NO;
        [card addSubview:list];
        [NSLayoutConstraint activateConstraints:@[
            [list.topAnchor constraintEqualToAnchor:card.topAnchor],
            [list.bottomAnchor constraintEqualToAnchor:card.bottomAnchor],
            [list.leadingAnchor constraintEqualToAnchor:card.leadingAnchor],
            [list.trailingAnchor constraintEqualToAnchor:card.trailingAnchor],
        ]];
        [stack addArrangedSubview:card];
    }
}

- (UIView *)buildFooter {
    UILabel *l = [UILabel new];
    l.text = [NSString stringWithFormat:@"ResidentKit %s", RK_VERSION];
    l.font = [UIFont systemFontOfSize:12];
    l.textColor = [UIColor tertiaryLabelColor];
    l.textAlignment = NSTextAlignmentCenter;
    return l;
}

#pragma mark - 生命周期
- (void)viewWillAppear:(BOOL)animated {
    [super viewWillAppear:animated];
    [self reloadFeatures];
}

- (void)viewDidAppear:(BOOL)animated {
    [super viewDidAppear:animated];
    [self startTimer];
}

- (void)viewWillDisappear:(BOOL)animated {
    [super viewWillDisappear:animated];
    [self stopTimer];
}

- (void)appWillEnterForeground {
    if (self.viewIfLoaded.window) [self startTimer];
}

- (void)dealloc {
    [_timer invalidate];
    [[NSNotificationCenter defaultCenter] removeObserver:self];
}

- (void)startTimer {
    if (_timer) return;
    __weak typeof(self) weakSelf = self;
    _timer = [NSTimer timerWithTimeInterval:RKHomeRefreshInterval repeats:YES block:^(NSTimer *t) {
        [weakSelf tick];
    }];
    _timer.tolerance = 0.5;
    [[NSRunLoop mainRunLoop] addTimer:_timer forMode:NSRunLoopCommonModes];
    [self tick];
}

- (void)stopTimer {
    [_timer invalidate];
    _timer = nil;
}

- (void)tick {
    [_client refreshStatus];
    __weak typeof(self) weakSelf = self;
    [_client fetchMetrics:^(NSDictionary *m) {
        typeof(self) self_ = weakSelf;
        if (self_) [self_->_summary applyMetrics:m];
    }];
}

#pragma mark - 渲染
- (void)clientDidUpdate {
    BOOL up = _client.connected;
    NSDictionary *st = _client.status;

    _statusDot.backgroundColor = up ? UIColor.systemGreenColor : UIColor.systemOrangeColor;
    _statusLabel.text = !up ? @"未连接" : (_client.serviceEnabled ? @"运行中" : @"已暂停");
    _enableSwitch.enabled = up;
    if (up && st) _enableSwitch.on = _client.serviceEnabled;

    // status 每隔几秒刷新：仅在脉冲动画未运行时才启动，否则每次都会明显重播。
    BOOL pulsing = up && _client.serviceEnabled;
    if (!pulsing) [_statusDot.layer removeAnimationForKey:@"pulse"];
    if (pulsing && ![_statusDot.layer animationForKey:@"pulse"]) {
        CABasicAnimation *pulse = [CABasicAnimation animationWithKeyPath:@"opacity"];
        pulse.fromValue = @1; pulse.toValue = @0.35;
        pulse.duration = 1.0; pulse.autoreverses = YES; pulse.repeatCount = HUGE_VALF;
        pulse.timingFunction = [CAMediaTimingFunction functionWithName:kCAMediaTimingFunctionEaseInEaseOut];
        [_statusDot.layer addAnimation:pulse forKey:@"pulse"];
    }

    if (up && st) {
        NSString *line = [NSString stringWithFormat:@"PID %@  ·  v%@  ·  已运行 %@",
                          st[RKStatusPID], st[RKStatusVersion],
                          RKFormatDuration([st[RKStatusUptime] doubleValue])];
        _detailLabel.text = _client.versionWarning
            ? [line stringByAppendingFormat:@"\n⚠︎ %@", _client.versionWarning] : line;
    } else {
        _detailLabel.text = _client.lastError ?: @"正在连接服务…";
        [_summary applyMetrics:nil];
    }
    for (RKFeature *f in _features) [[_featureViews objectForKey:f] update];
}

- (void)featureNeedsUpdate:(RKFeature *)feature {
    [[_featureViews objectForKey:feature] update];
}

- (void)reloadFeatures {
    for (RKFeature *f in _features) [f reload];
}

#pragma mark - 操作
- (void)openMonitor {
    [self.navigationController pushViewController:[RKMonitorViewController new] animated:YES];
}

- (void)featureTapped:(RKFeatureView *)view {
    RKFeature *f = view.feature;
    if (!f.available || !f.selectable) return;
    UIViewController *vc = [f makeViewController];
    if (vc) [self.navigationController pushViewController:vc animated:YES];
}

- (void)pullToRefresh:(UIRefreshControl *)rc {
    [self tick];
    [self reloadFeatures];
    dispatch_after(dispatch_time(DISPATCH_TIME_NOW, (int64_t)(0.4 * NSEC_PER_SEC)),
                   dispatch_get_main_queue(), ^{ [rc endRefreshing]; });
}

- (void)toggle:(UISwitch *)sw {
    [[[UIImpactFeedbackGenerator alloc] initWithStyle:UIImpactFeedbackStyleLight] impactOccurred];
    __weak typeof(self) weakSelf = self;
    [_client setServiceEnabled:sw.on completion:^(BOOL ok, BOOL now) {
        typeof(self) self_ = weakSelf;
        if (self_) self_->_enableSwitch.on = now;   // 失败时回滚
    }];
}

@end
