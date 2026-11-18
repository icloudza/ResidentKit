// RKDeviceInfoFeature — 示例功能项：「设备标识」磁贴 + 详情页。新建功能项时可参考本文件。
//
// 演示：网格磁贴（RKFeatureStyleTile）、点进页面、经 self.client 调 daemon 模块取数据
// （net.link 的 status：设备 ID、连接状态、接入配对码），以及复制到剪贴板。
// 设备 ID 是设备向服务器申请接入时使用的标识，与控制台「设备」页里的一致。

#import "../Core/RKFeature.h"
#import "../UI/RKWidgets.h"
#import "../UI/RKPageViewController.h"
#import "../../common/RKProtocol.h"
#import <sys/sysctl.h>

static NSString *RKMachineModel(void) {
    char buf[64] = {0};
    size_t len = sizeof(buf) - 1;
    return sysctlbyname("hw.machine", buf, &len, NULL, 0) == 0 ? @(buf) : @"—";
}

@interface RKDeviceInfoViewController : RKPageViewController
@property (nonatomic, strong) RKClient *client;
@end

@interface RKDeviceInfoFeature : RKFeature
@property (nonatomic, copy) NSDictionary *link;   // net.link 的 status
@end

@implementation RKDeviceInfoFeature

RK_REGISTER_FEATURE()

+ (NSInteger)order { return 20; }
- (NSString *)title { return @"设备标识"; }
- (NSString *)symbolName { return @"person.text.rectangle"; }
- (BOOL)isSelectable { return YES; }

- (NSString *)subtitle {
    NSString *uid = self.link[@"device_id"];
    return uid.length >= 8 ? [uid substringToIndex:8] : @"设备 ID 与型号";
}

- (void)reload {
    [self.client invokeModule:@"net.link" command:@"status" arguments:nil
                   completion:^(NSDictionary *result, NSError *error) {
        if (result) self.link = result;
        [self setNeedsUpdate];
    }];
}

- (UIViewController *)makeViewController {
    RKDeviceInfoViewController *vc = [RKDeviceInfoViewController new];
    vc.client = self.client;
    return vc;
}

@end

@implementation RKDeviceInfoViewController {
    RKInfoRow *_uid, *_model, *_system, *_app, *_service, *_link;
    NSString *_deviceID;
}

- (void)viewDidLoad {
    [super viewDidLoad];
    self.title = @"设备标识";

    RKCardView *card = [[RKCardView alloc] initWithTitle:@"本机" symbol:@"iphone"];
    card.contentStack.spacing = 12;
    _uid     = [[RKInfoRow alloc] initWithKey:@"设备 ID" symbol:@"number"];
    _uid.valueLabel.font = [UIFont monospacedSystemFontOfSize:13 weight:UIFontWeightRegular];
    _model   = [[RKInfoRow alloc] initWithKey:@"型号" symbol:@"iphone"];
    _system  = [[RKInfoRow alloc] initWithKey:@"系统" symbol:@"gearshape"];
    _app     = [[RKInfoRow alloc] initWithKey:@"App 版本" symbol:@"app"];
    _service = [[RKInfoRow alloc] initWithKey:@"服务版本" symbol:@"shippingbox"];
    _link    = [[RKInfoRow alloc] initWithKey:@"服务器" symbol:@"antenna.radiowaves.left.and.right"];
    for (UIView *r in @[_uid, _model, _system, _app, _service, _link]) [card.contentStack addArrangedSubview:r];

    UIButton *copy = [UIButton buttonWithType:UIButtonTypeSystem];
    [copy setTitle:@"复制设备 ID" forState:UIControlStateNormal];
    copy.titleLabel.font = [UIFont systemFontOfSize:17 weight:UIFontWeightSemibold];
    copy.tintColor = RKColorService();
    copy.backgroundColor = [UIColor secondarySystemGroupedBackgroundColor];
    copy.layer.cornerRadius = 14;
    copy.layer.cornerCurve = kCACornerCurveContinuous;
    [copy.heightAnchor constraintEqualToConstant:50].active = YES;
    [copy addTarget:self action:@selector(copyID:) forControlEvents:UIControlEventTouchUpInside];

    [self.contentStack addArrangedSubview:card];
    [self.contentStack addArrangedSubview:copy];

    _model.valueLabel.text = RKMachineModel();
    _system.valueLabel.text = [NSString stringWithFormat:@"iOS %@", UIDevice.currentDevice.systemVersion];
    _app.valueLabel.text = @(RK_VERSION);
    [self reload];
}

- (void)reload {
    _service.valueLabel.text = self.client.status[RKStatusVersion] ?: @"—";
    __weak typeof(self) weakSelf = self;
    [self.client invokeModule:@"net.link" command:@"status" arguments:nil
                   completion:^(NSDictionary *s, NSError *error) {
        typeof(self) self_ = weakSelf;
        if (!self_ || !s) return;
        self_->_deviceID = s[@"device_id"];
        self_->_uid.valueLabel.text = self_->_deviceID.length ? self_->_deviceID : @"—";
        NSDictionary *en = [s[@"enroll"] isKindOfClass:NSDictionary.class] ? s[@"enroll"] : nil;
        NSString *code = en[@"code"];
        self_->_link.valueLabel.text = [s[@"state"] isEqual:@"connected"] ? @"已接入"
            : code.length ? [NSString stringWithFormat:@"等待批准 · 配对码 %@", code]
            : [s[@"has_token"] boolValue] ? @"未连接" : @"未接入";
    }];
}

- (void)copyID:(UIButton *)sender {
    if (!_deviceID.length) return;
    UIPasteboard.generalPasteboard.string = _deviceID;
    [sender setTitle:@"已复制" forState:UIControlStateNormal];
    dispatch_after(dispatch_time(DISPATCH_TIME_NOW, (int64_t)(1.5 * NSEC_PER_SEC)), dispatch_get_main_queue(), ^{
        [sender setTitle:@"复制设备 ID" forState:UIControlStateNormal];
    });
}

@end
