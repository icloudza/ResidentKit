// RKScannerViewController.m — 说明见头文件。

#import "RKScannerViewController.h"
#import "RKWidgets.h"
#import <AVFoundation/AVFoundation.h>

@interface RKScannerViewController () <AVCaptureMetadataOutputObjectsDelegate>
@end

@implementation RKScannerViewController {
    AVCaptureSession *_session;
    AVCaptureVideoPreviewLayer *_preview;
    AVCaptureDevice *_device;
    dispatch_queue_t _sessionQueue;
    UIView *_frame;
    UILabel *_hintLabel, *_toast;
    UIButton *_torch;
    UIView *_blocked;          // 无相机 / 未授权时的说明
    BOOL _handling;            // 正在处理一次识别结果，暂停回调
}

- (instancetype)init {
    if ((self = [super init])) {
        _hint = @"对准控制台里的接入二维码";
        self.modalPresentationStyle = UIModalPresentationFullScreen;
    }
    return self;
}

- (UIStatusBarStyle)preferredStatusBarStyle { return UIStatusBarStyleLightContent; }

- (void)viewDidLoad {
    [super viewDidLoad];
    self.view.backgroundColor = UIColor.blackColor;
    _sessionQueue = dispatch_queue_create("rk.scanner", DISPATCH_QUEUE_SERIAL);

    _frame = [UIView new];
    _frame.layer.borderColor = [UIColor colorWithWhite:1 alpha:0.9].CGColor;
    _frame.layer.borderWidth = 2;
    _frame.layer.cornerRadius = 22;
    _frame.layer.cornerCurve = kCACornerCurveContinuous;
    _frame.translatesAutoresizingMaskIntoConstraints = NO;
    [self.view addSubview:_frame];

    _hintLabel = [UILabel new];
    _hintLabel.text = _hint;
    _hintLabel.textColor = UIColor.whiteColor;
    _hintLabel.font = [UIFont systemFontOfSize:15 weight:UIFontWeightMedium];
    _hintLabel.textAlignment = NSTextAlignmentCenter;
    _hintLabel.numberOfLines = 0;
    _hintLabel.translatesAutoresizingMaskIntoConstraints = NO;
    [self.view addSubview:_hintLabel];

    _toast = [UILabel new];
    _toast.textColor = UIColor.whiteColor;
    _toast.backgroundColor = [UIColor colorWithWhite:0.15 alpha:0.9];
    _toast.font = [UIFont systemFontOfSize:14 weight:UIFontWeightSemibold];
    _toast.textAlignment = NSTextAlignmentCenter;
    _toast.layer.cornerRadius = 12;
    _toast.layer.masksToBounds = YES;
    _toast.alpha = 0;
    _toast.translatesAutoresizingMaskIntoConstraints = NO;
    [self.view addSubview:_toast];

    UIButton *close = [self roundButton:@"xmark" action:@selector(close)];
    _torch = [self roundButton:@"flashlight.off.fill" action:@selector(toggleTorch)];
    _torch.hidden = YES;

    UILayoutGuide *safe = self.view.safeAreaLayoutGuide;
    [NSLayoutConstraint activateConstraints:@[
        [_frame.centerXAnchor constraintEqualToAnchor:self.view.centerXAnchor],
        [_frame.centerYAnchor constraintEqualToAnchor:self.view.centerYAnchor constant:-40],
        [_frame.widthAnchor constraintEqualToAnchor:self.view.widthAnchor multiplier:0.68],
        [_frame.heightAnchor constraintEqualToAnchor:_frame.widthAnchor],
        [_hintLabel.topAnchor constraintEqualToAnchor:_frame.bottomAnchor constant:24],
        [_hintLabel.leadingAnchor constraintEqualToAnchor:safe.leadingAnchor constant:32],
        [_hintLabel.trailingAnchor constraintEqualToAnchor:safe.trailingAnchor constant:-32],
        [_toast.centerXAnchor constraintEqualToAnchor:self.view.centerXAnchor],
        [_toast.bottomAnchor constraintEqualToAnchor:_frame.topAnchor constant:-24],
        [_toast.heightAnchor constraintEqualToConstant:40],
        [_toast.widthAnchor constraintGreaterThanOrEqualToConstant:180],
        [close.topAnchor constraintEqualToAnchor:safe.topAnchor constant:12],
        [close.leadingAnchor constraintEqualToAnchor:safe.leadingAnchor constant:16],
        [_torch.bottomAnchor constraintEqualToAnchor:safe.bottomAnchor constant:-32],
        [_torch.centerXAnchor constraintEqualToAnchor:self.view.centerXAnchor],
    ]];
}

- (UIButton *)roundButton:(NSString *)symbol action:(SEL)action {
    UIButton *b = [UIButton buttonWithType:UIButtonTypeSystem];
    [b setImage:RKSymbolImage(symbol, 18, UIFontWeightSemibold) forState:UIControlStateNormal];
    b.tintColor = UIColor.whiteColor;
    b.backgroundColor = [UIColor colorWithWhite:0.2 alpha:0.75];
    b.layer.cornerRadius = 22;
    b.translatesAutoresizingMaskIntoConstraints = NO;
    [b.widthAnchor constraintEqualToConstant:44].active = YES;
    [b.heightAnchor constraintEqualToConstant:44].active = YES;
    [b addTarget:self action:action forControlEvents:UIControlEventTouchUpInside];
    [self.view addSubview:b];
    return b;
}

- (void)viewDidAppear:(BOOL)animated {
    [super viewDidAppear:animated];
    switch ([AVCaptureDevice authorizationStatusForMediaType:AVMediaTypeVideo]) {
        case AVAuthorizationStatusAuthorized:
            [self startCamera];
            break;
        case AVAuthorizationStatusNotDetermined: {
            __weak typeof(self) weakSelf = self;
            [AVCaptureDevice requestAccessForMediaType:AVMediaTypeVideo completionHandler:^(BOOL granted) {
                dispatch_async(dispatch_get_main_queue(), ^{
                    if (granted) [weakSelf startCamera];
                    else [weakSelf showBlocked:@"没有相机权限" detail:@"在「设置」里允许 ResidentKit 使用相机后再扫码。" settings:YES];
                });
            }];
            break;
        }
        default:
            [self showBlocked:@"没有相机权限" detail:@"在「设置」里允许 ResidentKit 使用相机后再扫码。" settings:YES];
    }
}

- (void)viewWillDisappear:(BOOL)animated {
    [super viewWillDisappear:animated];
    [self setTorch:NO];
    AVCaptureSession *s = _session;
    dispatch_async(_sessionQueue, ^{ [s stopRunning]; });
}

- (void)viewDidLayoutSubviews {
    [super viewDidLayoutSubviews];
    _preview.frame = self.view.bounds;
    [self updateRectOfInterest];
}

#pragma mark 相机

- (void)startCamera {
    if (_session) return;
    _device = [AVCaptureDevice defaultDeviceWithMediaType:AVMediaTypeVideo];
    NSError *err = nil;
    AVCaptureDeviceInput *input = _device ? [AVCaptureDeviceInput deviceInputWithDevice:_device error:&err] : nil;
    if (!input) {
        [self showBlocked:@"无法使用相机" detail:err.localizedDescription ?: @"这台设备没有可用的相机。" settings:NO];
        return;
    }
    _session = [AVCaptureSession new];
    AVCaptureMetadataOutput *output = [AVCaptureMetadataOutput new];
    if (![_session canAddInput:input] || ![_session canAddOutput:output]) {
        [self showBlocked:@"无法使用相机" detail:@"相机被占用，请稍后再试。" settings:NO];
        _session = nil;
        return;
    }
    [_session addInput:input];
    [_session addOutput:output];
    [output setMetadataObjectsDelegate:self queue:dispatch_get_main_queue()];
    if ([output.availableMetadataObjectTypes containsObject:AVMetadataObjectTypeQRCode])
        output.metadataObjectTypes = @[ AVMetadataObjectTypeQRCode ];

    _preview = [AVCaptureVideoPreviewLayer layerWithSession:_session];
    _preview.videoGravity = AVLayerVideoGravityResizeAspectFill;
    _preview.frame = self.view.bounds;
    [self.view.layer insertSublayer:_preview atIndex:0];
    _torch.hidden = !_device.hasTorch;

    AVCaptureSession *s = _session;
    __weak typeof(self) weakSelf = self;
    dispatch_async(_sessionQueue, ^{
        [s startRunning];
        dispatch_async(dispatch_get_main_queue(), ^{ [weakSelf updateRectOfInterest]; });
    });
}

// 只识别取景框内的码，避免扫到旁边别的二维码。
- (void)updateRectOfInterest {
    AVCaptureMetadataOutput *out = (AVCaptureMetadataOutput *)_session.outputs.firstObject;
    if (!out || !_preview || !_session.isRunning) return;
    CGRect r = [_preview metadataOutputRectOfInterestForRect:[_frame convertRect:_frame.bounds toView:self.view]];
    if (!CGRectIsEmpty(r)) out.rectOfInterest = r;
}

- (void)captureOutput:(AVCaptureOutput *)output didOutputMetadataObjects:(NSArray<__kindof AVMetadataObject *> *)objects
       fromConnection:(AVCaptureConnection *)connection {
    if (_handling) return;
    for (AVMetadataObject *o in objects) {
        if (![o isKindOfClass:AVMetadataMachineReadableCodeObject.class]) continue;
        NSString *text = ((AVMetadataMachineReadableCodeObject *)o).stringValue;
        if (!text.length) continue;
        _handling = YES;
        if (self.onScan && self.onScan(text)) {
            [[UINotificationFeedbackGenerator new] notificationOccurred:UINotificationFeedbackTypeSuccess];
            return;   // 由调用方关闭页面
        }
        [[UINotificationFeedbackGenerator new] notificationOccurred:UINotificationFeedbackTypeWarning];
        [self showToast:@"不是接入二维码"];
        __weak typeof(self) weakSelf = self;
        dispatch_after(dispatch_time(DISPATCH_TIME_NOW, (int64_t)(1.5 * NSEC_PER_SEC)), dispatch_get_main_queue(), ^{
            typeof(self) self_ = weakSelf;
            if (self_) self_->_handling = NO;
        });
        return;
    }
}

#pragma mark 操作

- (void)close { [self dismissViewControllerAnimated:YES completion:nil]; }

- (void)toggleTorch { [self setTorch:_device.torchMode != AVCaptureTorchModeOn]; }

- (void)setTorch:(BOOL)on {
    if (!_device.hasTorch || ![_device lockForConfiguration:nil]) return;
    _device.torchMode = on ? AVCaptureTorchModeOn : AVCaptureTorchModeOff;
    [_device unlockForConfiguration];
    [_torch setImage:RKSymbolImage(on ? @"flashlight.on.fill" : @"flashlight.off.fill", 18, UIFontWeightSemibold)
            forState:UIControlStateNormal];
}

- (void)showToast:(NSString *)text {
    _toast.text = [NSString stringWithFormat:@"  %@  ", text];
    [UIView animateWithDuration:0.2 animations:^{ self->_toast.alpha = 1; } completion:^(BOOL f) {
        [UIView animateWithDuration:0.3 delay:1.2 options:0 animations:^{ self->_toast.alpha = 0; } completion:nil];
    }];
}

- (void)showBlocked:(NSString *)title detail:(NSString *)detail settings:(BOOL)settings {
    if (_blocked) return;
    _frame.hidden = _hintLabel.hidden = YES;
    UILabel *t = [UILabel new];
    t.text = title;
    t.textColor = UIColor.whiteColor;
    t.font = [UIFont systemFontOfSize:20 weight:UIFontWeightSemibold];
    UILabel *d = [UILabel new];
    d.text = detail;
    d.textColor = [UIColor colorWithWhite:1 alpha:0.7];
    d.font = [UIFont systemFontOfSize:15];
    d.numberOfLines = 0;
    d.textAlignment = NSTextAlignmentCenter;
    UIStackView *stack = [[UIStackView alloc] initWithArrangedSubviews:@[t, d]];
    stack.axis = UILayoutConstraintAxisVertical;
    stack.alignment = UIStackViewAlignmentCenter;
    stack.spacing = 10;
    if (settings) {
        UIButton *b = [UIButton buttonWithType:UIButtonTypeSystem];
        [b setTitle:@"去设置" forState:UIControlStateNormal];
        b.titleLabel.font = [UIFont systemFontOfSize:16 weight:UIFontWeightSemibold];
        [b addTarget:self action:@selector(openSettings) forControlEvents:UIControlEventTouchUpInside];
        [stack setCustomSpacing:18 afterView:d];
        [stack addArrangedSubview:b];
    }
    stack.translatesAutoresizingMaskIntoConstraints = NO;
    [self.view addSubview:stack];
    [NSLayoutConstraint activateConstraints:@[
        [stack.centerYAnchor constraintEqualToAnchor:self.view.centerYAnchor],
        [stack.leadingAnchor constraintEqualToAnchor:self.view.leadingAnchor constant:32],
        [stack.trailingAnchor constraintEqualToAnchor:self.view.trailingAnchor constant:-32],
    ]];
    _blocked = stack;
}

- (void)openSettings {
    [UIApplication.sharedApplication openURL:[NSURL URLWithString:UIApplicationOpenSettingsURLString]
                                     options:@{} completionHandler:nil];
}

@end
