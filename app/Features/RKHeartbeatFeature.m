// RKHeartbeatFeature — 示例功能项，新建功能项时可复制本文件。
//
// 对应 daemon 的 example.heartbeat 模块（daemon/Modules/RKHeartbeatModule.m），演示「带开关、
// 可点进详情页」的列表行：开关 → 模块命令 "set"；详情页 → "status" / "reset"。
// 删除本文件和该模块即可移除示例。

#import "../Core/RKFeature.h"
#import "../UI/RKWidgets.h"
#import "../UI/RKPageViewController.h"

static NSString *const RKHeartbeatModuleID = @"example.heartbeat";

// 共享缓存：列表行与详情页显示同样的数值。
@interface RKHeartbeatModel : NSObject
@property (nonatomic, copy) NSDictionary *status;
+ (instancetype)shared;
- (void)reloadWithClient:(RKClient *)client completion:(void (^)(void))done;
@end

@implementation RKHeartbeatModel
+ (instancetype)shared {
    static RKHeartbeatModel *m;
    static dispatch_once_t once;
    dispatch_once(&once, ^{ m = [RKHeartbeatModel new]; });
    return m;
}
- (void)reloadWithClient:(RKClient *)client completion:(void (^)(void))done {
    [client invokeModule:RKHeartbeatModuleID command:@"status" arguments:nil
              completion:^(NSDictionary *result, NSError *error) {
        if (result) self.status = result;
        done();
    }];
}
@end

#pragma mark - 列表行（带开关，点击进入详情）
@interface RKHeartbeatViewController : RKPageViewController
@property (nonatomic, strong) RKClient *client;
@end

@interface RKHeartbeatSwitchFeature : RKFeature
@end

@implementation RKHeartbeatSwitchFeature

RK_REGISTER_FEATURE()

+ (NSInteger)order { return 10; }
- (RKFeatureStyle)style { return RKFeatureStyleRow; }
- (NSString *)title { return @"心跳计数"; }
- (NSString *)symbolName { return @"heart.text.square"; }
- (BOOL)showsSwitch { return YES; }
- (BOOL)isOn { return [RKHeartbeatModel.shared.status[@"on"] boolValue]; }
- (BOOL)isSelectable { return YES; }

- (NSString *)subtitle {
    NSDictionary *s = RKHeartbeatModel.shared.status;
    if (!s) return @"示例模块";
    if (![s[@"on"] boolValue]) return @"已关闭";
    if (![s[@"running"] boolValue]) return @"常驻服务已暂停";
    return [NSString stringWithFormat:@"每 %@ 秒计一次 · 已计 %@ 次", s[@"interval"], s[@"beats"]];
}

- (UIViewController *)makeViewController {
    RKHeartbeatViewController *vc = [RKHeartbeatViewController new];
    vc.client = self.client;
    return vc;
}

- (void)reload {
    [RKHeartbeatModel.shared reloadWithClient:self.client completion:^{ [self setNeedsUpdate]; }];
}

- (void)switchDidChange:(BOOL)on {
    [self.client invokeModule:RKHeartbeatModuleID command:@"set" arguments:@{ @"on": @(on) }
                   completion:^(NSDictionary *result, NSError *error) {
        if (result) RKHeartbeatModel.shared.status = result;
        [self setNeedsUpdate];   // 失败时开关回弹到 -isOn
    }];
}

@end

#pragma mark - 详情页
@implementation RKHeartbeatViewController {
    RKInfoRow *_state, *_beats, *_since;
}

- (void)viewDidLoad {
    [super viewDidLoad];
    self.title = @"心跳详情";

    RKCardView *card = [[RKCardView alloc] initWithTitle:@"example.heartbeat" symbol:@"waveform.path"];
    card.contentStack.spacing = 12;
    _state = [[RKInfoRow alloc] initWithKey:@"状态" symbol:@"power"];
    _beats = [[RKInfoRow alloc] initWithKey:@"计数" symbol:@"number"];
    _since = [[RKInfoRow alloc] initWithKey:@"开始于" symbol:@"clock"];
    for (UIView *r in @[_state, _beats, _since]) [card.contentStack addArrangedSubview:r];

    UIButton *reset = [UIButton buttonWithType:UIButtonTypeSystem];
    [reset setTitle:@"清零" forState:UIControlStateNormal];
    reset.titleLabel.font = [UIFont systemFontOfSize:17 weight:UIFontWeightSemibold];
    reset.tintColor = RKColorService();
    reset.backgroundColor = [UIColor secondarySystemGroupedBackgroundColor];
    reset.layer.cornerRadius = 14;
    reset.layer.cornerCurve = kCACornerCurveContinuous;
    [reset.heightAnchor constraintEqualToConstant:50].active = YES;
    [reset addTarget:self action:@selector(reset) forControlEvents:UIControlEventTouchUpInside];

    [self.contentStack addArrangedSubview:card];
    [self.contentStack addArrangedSubview:reset];

    [[NSNotificationCenter defaultCenter] addObserver:self selector:@selector(reload)
        name:RKClientDaemonDidChangeNotification object:self.client];
    [self render];
}

- (void)dealloc { [[NSNotificationCenter defaultCenter] removeObserver:self]; }

- (void)viewWillAppear:(BOOL)animated {
    [super viewWillAppear:animated];
    [self reload];
}

- (void)reload {
    __weak typeof(self) weakSelf = self;
    [RKHeartbeatModel.shared reloadWithClient:self.client completion:^{ [weakSelf render]; }];
}

- (void)render {
    NSDictionary *s = RKHeartbeatModel.shared.status;
    if (!s) return;
    BOOL running = [s[@"running"] boolValue];
    _state.dot.hidden = NO;
    _state.dot.backgroundColor = running ? UIColor.systemGreenColor : UIColor.systemGrayColor;
    _state.valueLabel.text = running ? @"计数中" : ([s[@"on"] boolValue] ? @"服务已暂停" : @"已关闭");
    _beats.valueLabel.text = [s[@"beats"] stringValue];
    NSDate *since = s[@"since"];
    _since.valueLabel.text = [since isKindOfClass:NSDate.class]
        ? [NSDateFormatter localizedStringFromDate:since dateStyle:NSDateFormatterNoStyle
                                         timeStyle:NSDateFormatterMediumStyle] : @"—";
}

- (void)reset {
    __weak typeof(self) weakSelf = self;
    [self.client invokeModule:RKHeartbeatModuleID command:@"reset" arguments:nil
                   completion:^(NSDictionary *result, NSError *error) {
        if (result) RKHeartbeatModel.shared.status = result;
        [weakSelf render];
    }];
}

@end
