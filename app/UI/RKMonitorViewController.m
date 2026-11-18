// RKMonitorViewController.m — 说明见头文件。

#import "RKMonitorViewController.h"
#import "RKWidgets.h"
#import "../Core/RKClient.h"
#import "../../common/RKProtocol.h"

@implementation RKMonitorViewController {
    RKClient *_client;
    NSTimer *_timer;

    RKCardView *_memCard, *_cpuCard;
    RKRingView *_memService, *_memDevice, *_cpuService, *_cpuDevice;
    RKSparklineView *_memChart, *_cpuChart;
    RKInfoRow *_rowBand, *_rowPressure, *_rowPeak, *_rowThreads, *_rowWatchdog, *_rowKeepAlive;
}

- (void)viewDidLoad {
    [super viewDidLoad];
    _client = [RKClient shared];
    self.title = @"性能监控";

    for (UIView *card in @[[self buildMemoryCard], [self buildCPUCard], [self buildGuardCard]])
        [self.contentStack addArrangedSubview:card];

    NSNotificationCenter *nc = [NSNotificationCenter defaultCenter];
    [nc addObserver:self selector:@selector(applyStatus)
               name:RKClientDidUpdateNotification object:_client];
    [nc addObserver:self selector:@selector(appWillEnterForeground)
               name:UIApplicationWillEnterForegroundNotification object:nil];
    [nc addObserver:self selector:@selector(stopTimer)
               name:UIApplicationDidEnterBackgroundNotification object:nil];
    [self applyStatus];
}

#pragma mark - 卡片
- (UIStackView *)ringPairLeft:(RKRingView *)a right:(RKRingView *)b {
    UIStackView *s = [[UIStackView alloc] initWithArrangedSubviews:@[a, b]];
    s.distribution = UIStackViewDistributionFillEqually;
    return s;
}

- (UIView *)chartBlock:(RKSparklineView *)chart {
    [chart.heightAnchor constraintEqualToConstant:72].active = YES;
    UIStackView *legend = [[UIStackView alloc] initWithArrangedSubviews:@[
        RKLegendItem(@"服务", RKColorService()), RKLegendItem(@"设备", RKColorDevice()),
        [UIView new], RKLegendItem(@"近 60 秒", UIColor.clearColor)]];
    legend.spacing = 14;
    UIStackView *s = [[UIStackView alloc] initWithArrangedSubviews:@[chart, legend]];
    s.axis = UILayoutConstraintAxisVertical;
    s.spacing = 8;
    return s;
}

- (UIView *)buildMemoryCard {
    _memCard = [[RKCardView alloc] initWithTitle:@"内存" symbol:@"memorychip"];
    _memService = [[RKRingView alloc] initWithTitle:@"服务" color:RKColorService()];
    _memDevice  = [[RKRingView alloc] initWithTitle:@"设备" color:RKColorDevice()];
    _memChart = [RKSparklineView new];
    [_memCard.contentStack addArrangedSubview:[self ringPairLeft:_memService right:_memDevice]];
    [_memCard.contentStack addArrangedSubview:[self chartBlock:_memChart]];
    return _memCard;
}

- (UIView *)buildCPUCard {
    _cpuCard = [[RKCardView alloc] initWithTitle:@"CPU" symbol:@"cpu"];
    _cpuService = [[RKRingView alloc] initWithTitle:@"服务" color:RKColorService()];
    _cpuDevice  = [[RKRingView alloc] initWithTitle:@"设备" color:RKColorDevice()];
    _cpuChart = [RKSparklineView new];
    _cpuChart.autoScale = YES;
    [_cpuCard.contentStack addArrangedSubview:[self ringPairLeft:_cpuService right:_cpuDevice]];
    [_cpuCard.contentStack addArrangedSubview:[self chartBlock:_cpuChart]];
    return _cpuCard;
}

- (UIView *)buildGuardCard {
    RKCardView *c = [[RKCardView alloc] initWithTitle:@"守护" symbol:@"shield.lefthalf.filled"];
    c.contentStack.spacing = 12;
    _rowBand      = [[RKInfoRow alloc] initWithKey:@"Jetsam 优先级" symbol:@"arrow.up.circle"];
    _rowPressure  = [[RKInfoRow alloc] initWithKey:@"内存压力" symbol:@"gauge.medium"];
    _rowPeak      = [[RKInfoRow alloc] initWithKey:@"服务峰值内存" symbol:@"chart.line.uptrend.xyaxis"];
    _rowThreads   = [[RKInfoRow alloc] initWithKey:@"服务线程数" symbol:@"square.stack.3d.up"];
    _rowWatchdog  = [[RKInfoRow alloc] initWithKey:@"看门狗" symbol:@"timer"];
    _rowKeepAlive = [[RKInfoRow alloc] initWithKey:@"保活" symbol:@"arrow.clockwise.circle"];
    for (UIView *r in @[_rowBand, _rowPressure, _rowPeak, _rowThreads, _rowWatchdog, _rowKeepAlive]) {
        [c.contentStack addArrangedSubview:r];
    }
    return c;
}

#pragma mark - 生命周期
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
    _timer = [NSTimer timerWithTimeInterval:1.0 repeats:YES block:^(NSTimer *t) { [weakSelf tick]; }];
    _timer.tolerance = 0.2;
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
    [_client fetchMetrics:^(NSDictionary *m) { [weakSelf applyMetrics:m]; }];
}

#pragma mark - 渲染
- (void)applyStatus {
    NSDictionary *st = _client.status;
    if (!_client.connected || !st) {
        _rowWatchdog.valueLabel.text = @"—"; _rowWatchdog.dot.hidden = YES;
        _rowKeepAlive.valueLabel.text = @"—"; _rowKeepAlive.dot.hidden = YES;
        [self applyMetrics:nil];
        return;
    }
    [_rowWatchdog setHealthy:[st[RKStatusWatchdogOK] boolValue]];
    [_rowKeepAlive setHealthy:[st[RKStatusKeepAliveOK] boolValue]];
}

- (void)applyMetrics:(NSDictionary *)m {
    if (!m) {
        for (RKRingView *r in @[_memService, _memDevice, _cpuService, _cpuDevice]) {
            r.valueLabel.text = @"—"; r.captionLabel.text = @""; r.progress = 0;
        }
        _memCard.accessoryLabel.text = @""; _cpuCard.accessoryLabel.text = @"";
        [_memChart reset]; [_cpuChart reset];
        return;
    }
    // ---- 内存
    double fp = [m[RKMetricServiceFootprint] doubleValue];
    double limit = [m[RKMetricServiceLimit] doubleValue];
    double used = [m[RKMetricDeviceMemUsed] doubleValue];
    double total = [m[RKMetricDeviceMemTotal] doubleValue];
    double svcMemPct = limit > 0 ? fp / limit * 100 : 0;
    double devMemPct = total > 0 ? used / total * 100 : 0;

    _memService.valueLabel.text = RKFormatBytes(fp);
    _memService.captionLabel.text = limit > 0 ? [@"上限 " stringByAppendingString:RKFormatBytes(limit)] : @"无上限";
    _memService.progress = svcMemPct / 100;
    _memDevice.valueLabel.text = RKFormatPercent(devMemPct);
    _memDevice.captionLabel.text = [NSString stringWithFormat:@"%@ / %@", RKFormatBytes(used), RKFormatBytes(total)];
    _memDevice.progress = devMemPct / 100;
    _memCard.accessoryLabel.text = total > 0
        ? [NSString stringWithFormat:@"服务占整机 %@", RKFormatPercent(fp / total * 100)] : @"";
    [_memChart appendPrimary:svcMemPct secondary:devMemPct];

    // ---- CPU
    double svcCPU = [m[RKMetricServiceCPU] doubleValue];     // 占单核的百分比
    double devCPU = [m[RKMetricDeviceCPU] doubleValue];      // 占整机的百分比
    double cores = MAX(1, [m[RKMetricDeviceCores] doubleValue]);
    _cpuService.valueLabel.text = RKFormatPercent(svcCPU);
    _cpuService.captionLabel.text = @"单核占用";
    _cpuService.progress = svcCPU / 100;
    _cpuDevice.valueLabel.text = RKFormatPercent(devCPU);
    _cpuDevice.captionLabel.text = [NSString stringWithFormat:@"%.0f 核", cores];
    _cpuDevice.progress = devCPU / 100;
    _cpuCard.accessoryLabel.text = [NSString stringWithFormat:@"服务占整机 %@", RKFormatPercent(svcCPU / cores)];
    [_cpuChart appendPrimary:svcCPU / cores secondary:devCPU];

    // ---- 守护状态
    int band = [m[RKMetricJetsamBand] intValue];
    _rowBand.valueLabel.text = band >= 140 ? [NSString stringWithFormat:@"%d · 系统级", band]
                             : band >= 100 ? [NSString stringWithFormat:@"%d · 前台级", band]
                                           : [NSString stringWithFormat:@"%d · 后台级", band];
    int pressure = [m[RKMetricMemoryPressure] intValue];
    _rowPressure.dot.hidden = NO;
    if (pressure >= 4) {
        _rowPressure.valueLabel.text = @"严重"; _rowPressure.dot.backgroundColor = UIColor.systemRedColor;
    } else if (pressure >= 2) {
        _rowPressure.valueLabel.text = @"警告"; _rowPressure.dot.backgroundColor = UIColor.systemOrangeColor;
    } else {
        _rowPressure.valueLabel.text = @"正常"; _rowPressure.dot.backgroundColor = UIColor.systemGreenColor;
    }
    _rowPeak.valueLabel.text = RKFormatBytes([m[RKMetricServicePeak] doubleValue]);
    _rowThreads.valueLabel.text = [m[RKMetricServiceThreads] stringValue];
}

@end
