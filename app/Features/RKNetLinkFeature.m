// RKNetLinkFeature — 「服务器连接」：daemon net.link 模块（daemon/Modules/RKNetLinkModule.m）的 App 端。
//
// 首页行：开关 + 实时状态（"已连接 · 42 ms"）。点击打开 RKNetLinkViewController：
// 连接详情、锁屏保活开关、服务器地址，以及最近流量。
// 令牌无需填写：设备自动申请接入，管理员在控制台同意后由 daemon 领取（docs/enroll.md）。

#import "../Core/RKFeature.h"
#import "../UI/RKWidgets.h"
#import "../UI/RKPageViewController.h"
#import "../UI/RKScannerViewController.h"
#import "../Core/RKEnrollLink.h"

static NSString *const RKLinkModuleID = @"net.link";

@interface RKLinkModel : NSObject
@property (nonatomic, copy) NSDictionary *status;
+ (instancetype)shared;
- (void)reloadWithClient:(RKClient *)client completion:(void (^)(void))done;
@end

@implementation RKLinkModel
+ (instancetype)shared {
    static RKLinkModel *m;
    static dispatch_once_t once;
    dispatch_once(&once, ^{ m = [RKLinkModel new]; });
    return m;
}
- (void)reloadWithClient:(RKClient *)client completion:(void (^)(void))done {
    [client invokeModule:RKLinkModuleID command:@"status" arguments:nil
              completion:^(NSDictionary *result, NSError *error) {
        if (result) self.status = result;
        if (done) done();
    }];
}
@end

static NSString *RKLinkStateText(NSDictionary *s) {
    NSString *st = s[@"state"];
    // 自动接入中（尚无令牌）：显示申请进度与配对码，与控制台弹窗核对。
    NSDictionary *en = [s[@"enroll"] isKindOfClass:NSDictionary.class] ? s[@"enroll"] : nil;
    NSString *es = en[@"state"];
    if ([es isEqualToString:@"pending"]) {
        NSString *code = en[@"code"];
        return code.length == 6 ? [NSString stringWithFormat:@"等待控制台批准 · 配对码 %@ %@",
            [code substringToIndex:3], [code substringFromIndex:3]] : @"等待控制台批准";
    }
    if ([es isEqualToString:@"requesting"]) return @"正在申请接入…";
    if ([es isEqualToString:@"rejected"])   return @"接入被拒绝，稍后重试";
    if ([es isEqualToString:@"error"])      return [NSString stringWithFormat:@"接入失败：%@", en[@"last_error"] ?: @"未知错误"];
    if ([st isEqualToString:@"connected"])    return @"已连接";
    if ([st isEqualToString:@"connecting"])   return @"连接中…";
    if ([st isEqualToString:@"retrying"])     return [NSString stringWithFormat:@"重连中（第 %@ 次）", s[@"attempt"]];
    if ([st isEqualToString:@"no_network"])   return @"无网络";
    if ([st isEqualToString:@"unconfigured"]) return @"未配置服务器";
    return @"已关闭";
}

static UIColor *RKLinkStateColor(NSDictionary *s) {
    NSString *st = s[@"state"];
    NSString *es = [s[@"enroll"] isKindOfClass:NSDictionary.class] ? s[@"enroll"][@"state"] : nil;
    if ([es isEqualToString:@"pending"] || [es isEqualToString:@"requesting"]) return UIColor.systemOrangeColor;
    if ([st isEqualToString:@"connected"]) return UIColor.systemGreenColor;
    if ([st isEqualToString:@"connecting"] || [st isEqualToString:@"retrying"]) return UIColor.systemOrangeColor;
    if ([st isEqualToString:@"off"]) return UIColor.systemGrayColor;
    return UIColor.systemRedColor;
}

#pragma mark - 首页行
@interface RKNetLinkViewController : RKPageViewController
@property (nonatomic, strong) RKClient *client;
@end

@interface RKNetLinkFeature : RKFeature
@end

@implementation RKNetLinkFeature

RK_REGISTER_FEATURE()

+ (NSInteger)order { return 0; }
- (RKFeatureStyle)style { return RKFeatureStyleRow; }
- (NSString *)title { return @"服务器连接"; }
- (NSString *)symbolName { return @"antenna.radiowaves.left.and.right"; }
- (BOOL)showsSwitch { return YES; }
- (BOOL)isSelectable { return YES; }
- (BOOL)isOn { return [RKLinkModel.shared.status[@"on"] boolValue]; }

- (NSString *)subtitle {
    NSDictionary *s = RKLinkModel.shared.status;
    if (!s) return @"WebSocket 长连接";
    NSString *text = RKLinkStateText(s);
    if ([s[@"state"] isEqualToString:@"connected"] && s[@"rtt_ms"])
        text = [text stringByAppendingFormat:@" · %@ ms", s[@"rtt_ms"]];
    if ([s[@"queued"] integerValue] > 0)
        text = [text stringByAppendingFormat:@" · 待发 %@", s[@"queued"]];
    return text;
}

- (void)reload {
    [RKLinkModel.shared reloadWithClient:self.client completion:^{ [self setNeedsUpdate]; }];
}

- (void)switchDidChange:(BOOL)on {
    [self.client invokeModule:RKLinkModuleID command:@"set" arguments:@{ @"on": @(on) }
                   completion:^(NSDictionary *result, NSError *error) {
        if (result) RKLinkModel.shared.status = result;
        [self setNeedsUpdate];
    }];
}

- (UIViewController *)makeViewController {
    RKNetLinkViewController *vc = [RKNetLinkViewController new];
    vc.client = self.client;
    return vc;
}

@end

#pragma mark - 详情页
@implementation RKNetLinkViewController {
    NSTimer *_timer;
    RKInfoRow *_state, *_network, *_rtt, *_uptime, *_queued, *_traffic, *_reconnects, *_error;
    UISwitch *_awakeSwitch;
    UILabel *_awakeNote;
    UITextField *_urlField;
    RKInfoRow *_security;
    UIStackView *_recentStack;
    UILabel *_recentEmpty;
    NSArray *_shownRecent;   // 上次渲染的列表，没变就不重建
}

- (void)viewDidLoad {
    [super viewDidLoad];
    self.title = @"服务器连接";

    for (UIView *card in @[[self buildStatusCard], [self buildAwakeCard], [self buildServerCard], [self buildRecentCard]])
        [self.contentStack addArrangedSubview:card];

    [[NSNotificationCenter defaultCenter] addObserver:self selector:@selector(reload)
        name:RKClientDaemonDidChangeNotification object:self.client];
    [self render];
}

- (void)dealloc {
    [_timer invalidate];
    [[NSNotificationCenter defaultCenter] removeObserver:self];
}

#pragma mark 卡片
- (UIView *)buildStatusCard {
    RKCardView *c = [[RKCardView alloc] initWithTitle:@"连接" symbol:@"antenna.radiowaves.left.and.right"];
    c.contentStack.spacing = 12;
    _state      = [[RKInfoRow alloc] initWithKey:@"状态" symbol:@"circle.dashed"];
    _network    = [[RKInfoRow alloc] initWithKey:@"网络" symbol:@"wifi"];
    _rtt        = [[RKInfoRow alloc] initWithKey:@"延迟" symbol:@"stopwatch"];
    _uptime     = [[RKInfoRow alloc] initWithKey:@"已连接" symbol:@"clock"];
    _queued     = [[RKInfoRow alloc] initWithKey:@"待发送" symbol:@"tray.full"];
    _traffic    = [[RKInfoRow alloc] initWithKey:@"发送 / 接收" symbol:@"arrow.up.arrow.down"];
    _reconnects = [[RKInfoRow alloc] initWithKey:@"重连次数" symbol:@"arrow.triangle.2.circlepath"];
    _security   = [[RKInfoRow alloc] initWithKey:@"加密" symbol:@"lock"];
    _error      = [[RKInfoRow alloc] initWithKey:@"最近错误" symbol:@"exclamationmark.triangle"];
    _error.valueLabel.numberOfLines = 4;
    _error.valueLabel.lineBreakMode = NSLineBreakByTruncatingTail;
    for (UIView *r in @[_state, _security, _network, _rtt, _uptime, _queued, _traffic, _reconnects, _error])
        [c.contentStack addArrangedSubview:r];
    return c;
}

- (UIView *)buildAwakeCard {
    RKCardView *c = [[RKCardView alloc] initWithTitle:@"锁屏保持连接" symbol:@"moon.zzz"];
    _awakeSwitch = [UISwitch new];
    _awakeSwitch.onTintColor = RKColorService();
    [_awakeSwitch addTarget:self action:@selector(awakeChanged:) forControlEvents:UIControlEventValueChanged];
    _awakeNote = [UILabel new];
    _awakeNote.font = [UIFont systemFontOfSize:13];
    _awakeNote.textColor = [UIColor secondaryLabelColor];
    _awakeNote.numberOfLines = 0;
    UIStackView *row = [[UIStackView alloc] initWithArrangedSubviews:@[_awakeNote, _awakeSwitch]];
    row.alignment = UIStackViewAlignmentCenter;
    row.spacing = 12;
    [c.contentStack addArrangedSubview:row];
    return c;
}

- (UITextField *)fieldWithPlaceholder:(NSString *)ph secure:(BOOL)secure {
    UITextField *f = [UITextField new];
    f.placeholder = ph;
    f.secureTextEntry = secure;
    f.autocapitalizationType = UITextAutocapitalizationTypeNone;
    f.autocorrectionType = UITextAutocorrectionTypeNo;
    f.keyboardType = secure ? UIKeyboardTypeASCIICapable : UIKeyboardTypeURL;
    f.clearButtonMode = UITextFieldViewModeWhileEditing;
    f.font = [UIFont monospacedSystemFontOfSize:14 weight:UIFontWeightRegular];
    f.backgroundColor = [UIColor tertiarySystemFillColor];
    f.layer.cornerRadius = 10;
    UIView *pad = [[UIView alloc] initWithFrame:CGRectMake(0, 0, 12, 1)];
    f.leftView = pad; f.leftViewMode = UITextFieldViewModeAlways;
    [f.heightAnchor constraintEqualToConstant:44].active = YES;
    return f;
}

- (UIButton *)buttonWithTitle:(NSString *)title action:(SEL)action filled:(BOOL)filled {
    UIButton *b = [UIButton buttonWithType:UIButtonTypeSystem];
    [b setTitle:title forState:UIControlStateNormal];
    b.titleLabel.font = [UIFont systemFontOfSize:16 weight:UIFontWeightSemibold];
    b.layer.cornerRadius = 12;
    b.layer.cornerCurve = kCACornerCurveContinuous;
    if (filled) {
        b.backgroundColor = RKColorService();
        [b setTitleColor:UIColor.whiteColor forState:UIControlStateNormal];
    } else {
        b.backgroundColor = [UIColor tertiarySystemFillColor];
        b.tintColor = RKColorService();
    }
    [b.heightAnchor constraintEqualToConstant:44].active = YES;
    [b addTarget:self action:action forControlEvents:UIControlEventTouchUpInside];
    return b;
}

- (UIView *)buildServerCard {
    RKCardView *c = [[RKCardView alloc] initWithTitle:@"服务器" symbol:@"server.rack"];
    c.contentStack.spacing = 10;
    _urlField = [self fieldWithPlaceholder:@"wss://域名/rk" secure:NO];
    UIStackView *buttons = [[UIStackView alloc] initWithArrangedSubviews:@[
        [self buttonWithTitle:@"扫码" action:@selector(scan) filled:NO],
        [self buttonWithTitle:@"立即重连" action:@selector(reconnect) filled:NO],
        [self buttonWithTitle:@"保存" action:@selector(save) filled:YES]]];
    buttons.spacing = 10;
    buttons.distribution = UIStackViewDistributionFillEqually;
    for (UIView *v in @[_urlField, buttons]) [c.contentStack addArrangedSubview:v];
    return c;
}

- (UIView *)buildRecentCard {
    RKCardView *c = [[RKCardView alloc] initWithTitle:@"最近消息" symbol:@"list.bullet.rectangle"];
    _recentStack = [UIStackView new];
    _recentStack.axis = UILayoutConstraintAxisVertical;
    _recentStack.spacing = 8;
    _recentEmpty = [UILabel new];
    _recentEmpty.text = @"暂无（心跳不计入）";
    _recentEmpty.font = [UIFont systemFontOfSize:14];
    _recentEmpty.textColor = [UIColor tertiaryLabelColor];
    [c.contentStack addArrangedSubview:_recentEmpty];
    [c.contentStack addArrangedSubview:_recentStack];
    return c;
}

#pragma mark 生命周期
- (void)viewWillAppear:(BOOL)animated {
    [super viewWillAppear:animated];
    _urlField.text = RKLinkModel.shared.status[@"url"];
    [self reload];
    __weak typeof(self) weakSelf = self;
    _timer = [NSTimer scheduledTimerWithTimeInterval:2 repeats:YES block:^(NSTimer *t) { [weakSelf reload]; }];
}

- (void)viewWillDisappear:(BOOL)animated {
    [super viewWillDisappear:animated];
    [_timer invalidate];
    _timer = nil;
}

- (void)reload {
    __weak typeof(self) weakSelf = self;
    [RKLinkModel.shared reloadWithClient:self.client completion:^{ [weakSelf render]; }];
}

#pragma mark 渲染
- (void)render {
    NSDictionary *s = RKLinkModel.shared.status;
    if (!s) return;
    BOOL connected = [s[@"state"] isEqualToString:@"connected"];
    double now = [NSDate date].timeIntervalSince1970;

    _state.dot.hidden = NO;
    _state.dot.backgroundColor = RKLinkStateColor(s);
    _state.valueLabel.text = RKLinkStateText(s);
    NSDictionary *nets = @{ @"wifi": @"Wi-Fi", @"cellular": @"蜂窝", @"wired": @"有线", @"other": @"其他", @"none": @"无" };
    _network.valueLabel.text = nets[s[@"network"]] ?: @"—";
    _rtt.valueLabel.text = connected && s[@"rtt_ms"] ? [NSString stringWithFormat:@"%@ ms", s[@"rtt_ms"]] : @"—";
    _uptime.valueLabel.text = connected ? RKFormatDuration(now - [s[@"connected_since"] doubleValue]) : @"—";
    _queued.valueLabel.text = [s[@"queued"] stringValue];
    _traffic.valueLabel.text = [NSString stringWithFormat:@"%@ / %@", s[@"sent"], s[@"received"]];
    _reconnects.valueLabel.text = [s[@"reconnects"] stringValue];
    _error.valueLabel.text = s[@"last_error"] ?: @"无";
    _error.hidden = s[@"last_error"] == nil;

    BOOL wss = [s[@"url"] hasPrefix:@"wss://"], pinned = [s[@"pin"] length] > 0, ca = [s[@"ca_trusted"] boolValue];
    _security.dot.hidden = NO;
    _security.dot.backgroundColor = wss ? UIColor.systemGreenColor : UIColor.systemOrangeColor;
    NSString *fp = pinned ? [[s[@"pin"] substringToIndex:8] uppercaseString] : nil;
    _security.valueLabel.text = !wss ? @"未加密（ws）"
        : ca ? @"TLS · 受信任的域名证书"
        : (pinned ? [NSString stringWithFormat:@"TLS · 证书指纹 %@…", fp] : @"TLS · 首次连接时信任");

    _awakeSwitch.on = [s[@"keep_awake"] boolValue];
    _awakeNote.text = !_awakeSwitch.on ? @"息屏后系统可能休眠，连接会在唤醒后恢复"
        : ([s[@"awake_held"] boolValue] ? @"已生效：息屏时阻止系统休眠，保持连接"
                                        : @"已开启，连接启用后生效");

    if (!_urlField.isFirstResponder && !_urlField.text.length) _urlField.text = s[@"url"];
    NSArray *recent = [s[@"recent"] isKindOfClass:NSArray.class] ? [[s[@"recent"] reverseObjectEnumerator] allObjects] : @[];
    _recentEmpty.hidden = recent.count > 0;
    if ([recent isEqualToArray:_shownRecent ?: @[]] && _shownRecent) return;   // 每 2 秒刷新一次，多数时候没有新帧
    _shownRecent = recent;
    for (UIView *v in _recentStack.arrangedSubviews) [v removeFromSuperview];
    static NSDateFormatter *fmt;
    if (!fmt) { fmt = [NSDateFormatter new]; fmt.dateFormat = @"HH:mm:ss"; }
    for (NSDictionary *m in [recent subarrayWithRange:NSMakeRange(0, MIN(recent.count, 10))]) {
        BOOL outgoing = [m[@"dir"] isEqualToString:@"out"];
        UILabel *time = [UILabel new];
        time.text = [fmt stringFromDate:[NSDate dateWithTimeIntervalSince1970:[m[@"t"] doubleValue]]];
        time.font = [UIFont monospacedDigitSystemFontOfSize:13 weight:UIFontWeightRegular];
        time.textColor = [UIColor secondaryLabelColor];
        UIImageView *arrow = [[UIImageView alloc] initWithImage:
            RKSymbolImage(outgoing ? @"arrow.up" : @"arrow.down", 12, UIFontWeightSemibold)];
        arrow.tintColor = outgoing ? RKColorService() : RKColorDevice();
        [arrow setContentHuggingPriority:UILayoutPriorityRequired forAxis:UILayoutConstraintAxisHorizontal];
        UILabel *type = [UILabel new];
        type.text = m[@"type"];
        type.font = [UIFont monospacedSystemFontOfSize:14 weight:UIFontWeightMedium];
        [time setContentHuggingPriority:UILayoutPriorityRequired forAxis:UILayoutConstraintAxisHorizontal];
        UIStackView *row = [[UIStackView alloc] initWithArrangedSubviews:@[time, arrow, type, [UIView new]]];
        row.spacing = 10;
        row.alignment = UIStackViewAlignmentCenter;
        [_recentStack addArrangedSubview:row];
    }
}

#pragma mark 操作
- (void)awakeChanged:(UISwitch *)sw {
    __weak typeof(self) weakSelf = self;
    [self.client invokeModule:RKLinkModuleID command:@"set" arguments:@{ @"keep_awake": @(sw.on) }
                   completion:^(NSDictionary *result, NSError *error) {
        if (result) RKLinkModel.shared.status = result;
        [weakSelf render];
    }];
}

// 扫控制台「添加设备」里的接入二维码：确认后写入地址（及指纹、接入码）并立即申请接入。
- (void)scan {
    [self.view endEditing:YES];
    RKScannerViewController *sc = [RKScannerViewController new];
    __weak typeof(self) weakSelf = self;
    __weak RKScannerViewController *weakSc = sc;
    sc.onScan = ^BOOL(NSString *text) {
        RKEnrollLink *link = [RKEnrollLink linkWithString:text];
        if (!link) return NO;
        [weakSc dismissViewControllerAnimated:YES completion:^{
            typeof(self) self_ = weakSelf;
            if (self_) [link confirmAndApplyFrom:self_];
        }];
        return YES;
    };
    [self presentViewController:sc animated:YES completion:nil];
}

- (void)save {
    [self.view endEditing:YES];
    NSMutableDictionary *args = [@{ @"url": _urlField.text ?: @"" } mutableCopy];
    __weak typeof(self) weakSelf = self;
    [self.client invokeModule:RKLinkModuleID command:@"configure" arguments:args
                   completion:^(NSDictionary *result, NSError *error) {
        typeof(self) self_ = weakSelf;
        if (!self_) return;
        if (error) {
            UIAlertController *a = [UIAlertController alertControllerWithTitle:@"保存失败"
                message:error.localizedDescription preferredStyle:UIAlertControllerStyleAlert];
            [a addAction:[UIAlertAction actionWithTitle:@"好" style:UIAlertActionStyleDefault handler:nil]];
            [self_ presentViewController:a animated:YES completion:nil];
            return;
        }
        RKLinkModel.shared.status = result;
        [self_ render];
    }];
}

- (void)reconnect {
    __weak typeof(self) weakSelf = self;
    [self.client invokeModule:RKLinkModuleID command:@"reconnect" arguments:nil
                   completion:^(NSDictionary *result, NSError *error) {
        if (result) RKLinkModel.shared.status = result;
        [weakSelf render];
    }];
}

@end
