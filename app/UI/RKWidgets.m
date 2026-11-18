// RKWidgets.m — 说明见头文件。

#import "RKWidgets.h"

static UIColor *RKDynamic(UIColor *light, UIColor *dark) {
    return [UIColor colorWithDynamicProvider:^UIColor *(UITraitCollection *tc) {
        return tc.userInterfaceStyle == UIUserInterfaceStyleDark ? dark : light;
    }];
}
UIColor *RKColorService(void) {
    return RKDynamic([UIColor colorWithRed:0.18 green:0.44 blue:0.93 alpha:1],   // #2E70ED
                     [UIColor colorWithRed:0.39 green:0.62 blue:1.00 alpha:1]);  // #639EFF
}
UIColor *RKColorDevice(void) {
    return RKDynamic([UIColor colorWithRed:0.47 green:0.53 blue:0.60 alpha:1],   // #788799
                     [UIColor colorWithRed:0.62 green:0.67 blue:0.74 alpha:1]);
}
UIColor *RKColorHeader(void) {
    return RKDynamic([UIColor colorWithRed:0.769 green:0.863 blue:1.000 alpha:1], // #C4DCFF
                     [UIColor colorWithRed:0.153 green:0.231 blue:0.353 alpha:1]); // #273B5A
}

#pragma mark - 格式化
NSString *RKFormatBytes(double b) {
    return [NSByteCountFormatter stringFromByteCount:(long long)b
                                          countStyle:NSByteCountFormatterCountStyleMemory];
}

NSString *RKFormatPercent(double p) {
    if (p > 0 && p < 0.1) return @"<0.1%";
    return [NSString stringWithFormat:(p < 10 ? @"%.1f%%" : @"%.0f%%"), p];
}

NSString *RKFormatDuration(double s) {
    long t = (long)s;
    if (t >= 86400) return [NSString stringWithFormat:@"%ld天%ld小时", t / 86400, (t % 86400) / 3600];
    if (t >= 3600)  return [NSString stringWithFormat:@"%ld小时%ld分", t / 3600, (t % 3600) / 60];
    if (t >= 60)    return [NSString stringWithFormat:@"%ld分%ld秒", t / 60, t % 60];
    return [NSString stringWithFormat:@"%ld秒", t];
}

NSAttributedString *RKValueWithUnit(NSString *text, CGFloat size) {
    NSRange split = [text rangeOfString:@" " options:NSBackwardsSearch];
    if (split.location == NSNotFound && [text hasSuffix:@"%"]) split = NSMakeRange(text.length - 1, 0);
    NSMutableAttributedString *s = [[NSMutableAttributedString alloc] initWithString:text attributes:@{
        NSFontAttributeName: [UIFont monospacedDigitSystemFontOfSize:size weight:UIFontWeightSemibold],
        NSForegroundColorAttributeName: [UIColor labelColor] }];
    if (split.location != NSNotFound) {
        NSRange unit = NSMakeRange(split.location, text.length - split.location);
        [s addAttributes:@{ NSFontAttributeName: [UIFont systemFontOfSize:size * 0.72 weight:UIFontWeightSemibold],
                            NSForegroundColorAttributeName: [UIColor secondaryLabelColor] } range:unit];
    }
    return s;
}

UIView *RKHairline(void) {
    UIView *v = [UIView new];
    v.backgroundColor = [UIColor separatorColor];
    [v.heightAnchor constraintEqualToConstant:1.0 / UIScreen.mainScreen.scale].active = YES;
    return v;
}

UIImage *RKSymbolImage(NSString *name, CGFloat size, UIFontWeight weight) {
    if (!name) return nil;
    UIImageSymbolConfiguration *cfg =
        [UIImageSymbolConfiguration configurationWithPointSize:size weight:
            weight >= UIFontWeightSemibold ? UIImageSymbolWeightSemibold : UIImageSymbolWeightRegular];
    return [UIImage systemImageNamed:name withConfiguration:cfg];
}

#pragma mark - 卡片
@implementation RKCardView

- (instancetype)initWithTitle:(NSString *)title symbol:(NSString *)sfSymbol {
    if ((self = [super initWithFrame:CGRectZero])) {
        self.backgroundColor = [UIColor secondarySystemGroupedBackgroundColor];
        self.layer.cornerRadius = 22;
        self.layer.cornerCurve = kCACornerCurveContinuous;

        UIImageView *icon = [[UIImageView alloc] initWithImage:RKSymbolImage(sfSymbol, 15, UIFontWeightSemibold)];
        icon.tintColor = RKColorService();
        icon.contentMode = UIViewContentModeScaleAspectFit;
        [icon setContentHuggingPriority:UILayoutPriorityRequired forAxis:UILayoutConstraintAxisHorizontal];

        _titleLabel = [UILabel new];
        _titleLabel.text = title;
        _titleLabel.font = [UIFont systemFontOfSize:17 weight:UIFontWeightSemibold];

        _accessoryLabel = [UILabel new];
        _accessoryLabel.font = [UIFont monospacedDigitSystemFontOfSize:13 weight:UIFontWeightMedium];
        _accessoryLabel.textColor = [UIColor secondaryLabelColor];
        _accessoryLabel.textAlignment = NSTextAlignmentRight;

        UIStackView *header = [[UIStackView alloc] initWithArrangedSubviews:
            sfSymbol ? @[icon, _titleLabel, _accessoryLabel] : @[_titleLabel, _accessoryLabel]];
        header.spacing = 8;
        header.alignment = UIStackViewAlignmentCenter;

        _contentStack = [[UIStackView alloc] initWithArrangedSubviews:@[header]];
        _contentStack.axis = UILayoutConstraintAxisVertical;
        _contentStack.spacing = 16;
        _contentStack.translatesAutoresizingMaskIntoConstraints = NO;
        [self addSubview:_contentStack];
        [NSLayoutConstraint activateConstraints:@[
            [_contentStack.topAnchor constraintEqualToAnchor:self.topAnchor constant:18],
            [_contentStack.bottomAnchor constraintEqualToAnchor:self.bottomAnchor constant:-18],
            [_contentStack.leadingAnchor constraintEqualToAnchor:self.leadingAnchor constant:18],
            [_contentStack.trailingAnchor constraintEqualToAnchor:self.trailingAnchor constant:-18],
        ]];
    }
    return self;
}

@end

#pragma mark - 环形图
@implementation RKRingView {
    CAShapeLayer *_track;
    CAShapeLayer *_arc;
    UIView *_ringBox;
}

- (instancetype)initWithTitle:(NSString *)title color:(UIColor *)color {
    if ((self = [super initWithFrame:CGRectZero])) {
        _color = color;

        _ringBox = [UIView new];
        _ringBox.translatesAutoresizingMaskIntoConstraints = NO;
        [self addSubview:_ringBox];

        _track = [CAShapeLayer layer];
        _track.fillColor = UIColor.clearColor.CGColor;
        _track.lineWidth = 10;
        _track.lineCap = kCALineCapRound;
        [_ringBox.layer addSublayer:_track];

        _arc = [CAShapeLayer layer];
        _arc.fillColor = UIColor.clearColor.CGColor;
        _arc.lineWidth = 10;
        _arc.lineCap = kCALineCapRound;
        _arc.strokeEnd = 0;
        [_ringBox.layer addSublayer:_arc];

        _valueLabel = [UILabel new];
        _valueLabel.font = [UIFont monospacedDigitSystemFontOfSize:19 weight:UIFontWeightBold];
        _valueLabel.textAlignment = NSTextAlignmentCenter;
        _valueLabel.adjustsFontSizeToFitWidth = YES;
        _valueLabel.minimumScaleFactor = 0.6;
        _valueLabel.text = @"—";

        _captionLabel = [UILabel new];
        _captionLabel.font = [UIFont systemFontOfSize:11 weight:UIFontWeightMedium];
        _captionLabel.textColor = [UIColor secondaryLabelColor];
        _captionLabel.textAlignment = NSTextAlignmentCenter;
        _captionLabel.adjustsFontSizeToFitWidth = YES;
        _captionLabel.minimumScaleFactor = 0.7;

        UIStackView *center = [[UIStackView alloc] initWithArrangedSubviews:@[_valueLabel, _captionLabel]];
        center.axis = UILayoutConstraintAxisVertical;
        center.alignment = UIStackViewAlignmentCenter;
        center.spacing = 1;
        center.translatesAutoresizingMaskIntoConstraints = NO;
        [_ringBox addSubview:center];

        _titleLabel = [UILabel new];
        _titleLabel.text = title;
        _titleLabel.font = [UIFont systemFontOfSize:13 weight:UIFontWeightSemibold];
        _titleLabel.textColor = color;
        _titleLabel.textAlignment = NSTextAlignmentCenter;
        _titleLabel.translatesAutoresizingMaskIntoConstraints = NO;
        [self addSubview:_titleLabel];

        [NSLayoutConstraint activateConstraints:@[
            [_ringBox.topAnchor constraintEqualToAnchor:self.topAnchor],
            [_ringBox.centerXAnchor constraintEqualToAnchor:self.centerXAnchor],
            [_ringBox.widthAnchor constraintEqualToConstant:118],
            [_ringBox.heightAnchor constraintEqualToConstant:118],
            [_ringBox.leadingAnchor constraintGreaterThanOrEqualToAnchor:self.leadingAnchor],
            [center.centerXAnchor constraintEqualToAnchor:_ringBox.centerXAnchor],
            [center.centerYAnchor constraintEqualToAnchor:_ringBox.centerYAnchor],
            [center.widthAnchor constraintLessThanOrEqualToConstant:84],
            [_valueLabel.widthAnchor constraintLessThanOrEqualToConstant:84],
            [_titleLabel.topAnchor constraintEqualToAnchor:_ringBox.bottomAnchor constant:8],
            [_titleLabel.centerXAnchor constraintEqualToAnchor:self.centerXAnchor],
            [_titleLabel.bottomAnchor constraintEqualToAnchor:self.bottomAnchor],
        ]];
        [self applyColors];
    }
    return self;
}

- (void)applyColors {
    UIColor *c = [_color resolvedColorWithTraitCollection:self.traitCollection];
    _arc.strokeColor = c.CGColor;
    _track.strokeColor = [c colorWithAlphaComponent:0.15].CGColor;
}

- (void)setColor:(UIColor *)color { _color = color; [self applyColors]; }

- (void)traitCollectionDidChange:(UITraitCollection *)previous {
    [super traitCollectionDidChange:previous];
    [self applyColors]; // CGColor 不会自动跟随深浅色模式
}

- (void)layoutSubviews {
    [super layoutSubviews];
    CGRect b = _ringBox.bounds;
    CGFloat r = MIN(b.size.width, b.size.height) / 2 - _arc.lineWidth / 2;
    UIBezierPath *p = [UIBezierPath bezierPathWithArcCenter:CGPointMake(CGRectGetMidX(b), CGRectGetMidY(b))
                                                     radius:r
                                                 startAngle:-M_PI_2
                                                   endAngle:-M_PI_2 + 2 * M_PI
                                                  clockwise:YES];
    _track.frame = b; _arc.frame = b;
    _track.path = p.CGPath; _arc.path = p.CGPath;
}

- (void)setProgress:(CGFloat)progress {
    _progress = MAX(0, MIN(1, progress));
    // 极小的非零值也显示一条细线，让「几乎为零」看起来是在工作而不是坏了。
    CGFloat shown = (_progress > 0 && _progress < 0.012) ? 0.012 : _progress;
    CABasicAnimation *a = [CABasicAnimation animationWithKeyPath:@"strokeEnd"];
    a.fromValue = @(_arc.presentationLayer ? _arc.presentationLayer.strokeEnd : _arc.strokeEnd);
    a.toValue = @(shown);
    a.duration = 0.6;
    a.timingFunction = [CAMediaTimingFunction functionWithName:kCAMediaTimingFunctionEaseOut];
    _arc.strokeEnd = shown;
    [_arc addAnimation:a forKey:@"progress"];
}

@end

#pragma mark - 折线图
@implementation RKSparklineView {
    NSMutableArray<NSNumber *> *_a, *_b;
    CAShapeLayer *_lineA, *_lineB, *_grid;
    CAShapeLayer *_fill;
}

- (instancetype)initWithFrame:(CGRect)frame {
    if ((self = [super initWithFrame:frame])) {
        _capacity = 60;
        _a = [NSMutableArray array]; _b = [NSMutableArray array];

        _grid = [CAShapeLayer layer];
        _grid.lineWidth = 1;
        _grid.lineDashPattern = @[@2, @4];
        _grid.fillColor = UIColor.clearColor.CGColor;
        [self.layer addSublayer:_grid];

        _fill = [CAShapeLayer layer];   // 服务曲线下方的半透明纯色填充
        [self.layer addSublayer:_fill];

        _lineB = [CAShapeLayer layer];
        _lineA = [CAShapeLayer layer];
        for (CAShapeLayer *l in @[_lineB, _lineA]) {
            l.fillColor = UIColor.clearColor.CGColor;
            l.lineWidth = 2;
            l.lineJoin = kCALineJoinRound;
            l.lineCap = kCALineCapRound;
            [self.layer addSublayer:l];
        }
        [self applyColors];
    }
    return self;
}

- (void)applyColors {
    UIColor *svc = [RKColorService() resolvedColorWithTraitCollection:self.traitCollection];
    UIColor *dev = [RKColorDevice() resolvedColorWithTraitCollection:self.traitCollection];
    _lineA.strokeColor = svc.CGColor;
    _lineB.strokeColor = dev.CGColor;
    _fill.fillColor = [svc colorWithAlphaComponent:0.12].CGColor;
    _grid.strokeColor = [UIColor separatorColor].CGColor;
}

- (void)traitCollectionDidChange:(UITraitCollection *)previous {
    [super traitCollectionDidChange:previous];
    [self applyColors];
}

- (void)reset { [_a removeAllObjects]; [_b removeAllObjects]; [self redraw]; }

- (void)appendPrimary:(double)primary secondary:(double)secondary {
    [_a addObject:@(primary)]; [_b addObject:@(secondary)];
    while (_a.count > _capacity) [_a removeObjectAtIndex:0];
    while (_b.count > _capacity) [_b removeObjectAtIndex:0];
    [self redraw];
}

- (void)layoutSubviews { [super layoutSubviews]; [self redraw]; }

- (double)scaleMax {
    if (!_autoScale) return 100;
    double m = 0;
    for (NSNumber *n in _a) m = MAX(m, n.doubleValue);
    for (NSNumber *n in _b) m = MAX(m, n.doubleValue);
    // 上限向上取整到「整齐」的刻度，避免图表每秒抖动。
    double steps[] = {5, 10, 25, 50, 100};
    for (int i = 0; i < 5; i++) if (m * 1.15 <= steps[i]) return steps[i];
    return 100;
}

- (UIBezierPath *)pathFor:(NSArray<NSNumber *> *)v max:(double)max {
    UIBezierPath *p = [UIBezierPath bezierPath];
    CGRect b = CGRectInset(self.bounds, 1, 2);
    if (v.count == 0 || b.size.width <= 0) return p;
    CGFloat step = b.size.width / (CGFloat)MAX(1, (NSInteger)_capacity - 1);
    CGFloat x0 = CGRectGetMaxX(b) - step * (v.count - 1);
    for (NSUInteger i = 0; i < v.count; i++) {
        double y01 = MAX(0, MIN(1, v[i].doubleValue / max));
        CGPoint pt = CGPointMake(x0 + step * i, CGRectGetMaxY(b) - y01 * b.size.height);
        if (i == 0) [p moveToPoint:pt]; else [p addLineToPoint:pt];
    }
    return p;
}

- (void)redraw {
    [CATransaction begin];
    [CATransaction setDisableActions:YES];
    CGRect b = self.bounds;
    for (CALayer *l in @[_grid, _fill, _lineA, _lineB]) l.frame = b;

    UIBezierPath *g = [UIBezierPath bezierPath];
    for (int i = 1; i <= 3; i++) {
        CGFloat y = round(b.size.height * i / 4.0) + 0.5;
        [g moveToPoint:CGPointMake(0, y)]; [g addLineToPoint:CGPointMake(b.size.width, y)];
    }
    _grid.path = g.CGPath;

    double max = [self scaleMax];
    UIBezierPath *pa = [self pathFor:_a max:max];
    _lineA.path = pa.CGPath;
    _lineB.path = [self pathFor:_b max:max].CGPath;

    UIBezierPath *fill = [pa copy];
    if (_a.count > 1) {
        [fill addLineToPoint:CGPointMake(fill.currentPoint.x, CGRectGetMaxY(b))];
        CGFloat step = (b.size.width - 2) / (CGFloat)MAX(1, (NSInteger)_capacity - 1);
        [fill addLineToPoint:CGPointMake(CGRectGetMaxX(b) - 1 - step * (_a.count - 1), CGRectGetMaxY(b))];
        [fill closePath];
    }
    _fill.path = fill.CGPath;
    [CATransaction commit];
}

@end

#pragma mark - 对比条
@implementation RKCompareBar {
    UIView *_device, *_service;
    double _s, _d;
}

- (instancetype)initWithFrame:(CGRect)frame {
    if ((self = [super initWithFrame:frame])) {
        self.backgroundColor = [UIColor tertiarySystemFillColor];
        self.clipsToBounds = YES;
        _device = [UIView new];
        _device.backgroundColor = [RKColorDevice() colorWithAlphaComponent:0.5];
        _service = [UIView new];
        _service.backgroundColor = RKColorService();
        [self addSubview:_device];
        [self addSubview:_service];
        [self.heightAnchor constraintEqualToConstant:4].active = YES;
    }
    return self;
}

- (void)setService:(double)service device:(double)device {
    _s = MAX(0, MIN(1, service));
    _d = MAX(0, MIN(1, device));
    [UIView animateWithDuration:0.5 delay:0 options:UIViewAnimationOptionBeginFromCurrentState
                     animations:^{ [self layoutIfNeeded]; [self layoutBars]; } completion:nil];
}

- (void)layoutBars {
    CGFloat w = self.bounds.size.width, h = self.bounds.size.height;
    // 极小的非零服务占比仍显示为一条细线。
    CGFloat sw = _s > 0 ? MAX(3, w * _s) : 0;
    _device.frame = CGRectMake(0, 0, w * _d, h);
    _service.frame = CGRectMake(0, 0, sw, h);
    _device.layer.cornerRadius = _service.layer.cornerRadius = h / 2;
}

- (void)layoutSubviews {
    [super layoutSubviews];
    self.layer.cornerRadius = self.bounds.size.height / 2;
    [self layoutBars];
}

@end

#pragma mark - 图例 / 信息行
UIView *RKLegendItem(NSString *text, UIColor *color) {
    UIView *dot = [UIView new];
    dot.backgroundColor = color;
    dot.layer.cornerRadius = 4;
    [dot.widthAnchor constraintEqualToConstant:8].active = YES;
    [dot.heightAnchor constraintEqualToConstant:8].active = YES;
    UILabel *l = [UILabel new];
    l.text = text;
    l.font = [UIFont systemFontOfSize:12 weight:UIFontWeightMedium];
    l.textColor = [UIColor secondaryLabelColor];
    UIStackView *s = [[UIStackView alloc] initWithArrangedSubviews:@[dot, l]];
    s.spacing = 6;
    s.alignment = UIStackViewAlignmentCenter;
    return s;
}

@implementation RKInfoRow

- (instancetype)initWithKey:(NSString *)key symbol:(NSString *)sfSymbol {
    if ((self = [super initWithFrame:CGRectZero])) {
        UIImageView *icon = [[UIImageView alloc] initWithImage:RKSymbolImage(sfSymbol, 14, UIFontWeightRegular)];
        icon.tintColor = [UIColor secondaryLabelColor];
        icon.contentMode = UIViewContentModeCenter;
        [icon.widthAnchor constraintEqualToConstant:22].active = YES;

        UILabel *k = [UILabel new];
        k.text = key;
        k.font = [UIFont systemFontOfSize:15];

        _dot = [UIView new];
        _dot.layer.cornerRadius = 4;
        _dot.hidden = YES;
        [_dot.widthAnchor constraintEqualToConstant:8].active = YES;
        [_dot.heightAnchor constraintEqualToConstant:8].active = YES;

        _valueLabel = [UILabel new];
        _valueLabel.font = [UIFont monospacedDigitSystemFontOfSize:15 weight:UIFontWeightMedium];
        _valueLabel.textColor = [UIColor secondaryLabelColor];
        _valueLabel.textAlignment = NSTextAlignmentRight;
        _valueLabel.text = @"—";
        _valueLabel.numberOfLines = 0;   // 过长（错误信息、设备 ID）时换行，不把整页撑宽
        _valueLabel.lineBreakMode = NSLineBreakByCharWrapping;
        // 标题保持完整，值让位并换行
        [k setContentCompressionResistancePriority:UILayoutPriorityRequired forAxis:UILayoutConstraintAxisHorizontal];
        [k setContentHuggingPriority:UILayoutPriorityRequired forAxis:UILayoutConstraintAxisHorizontal];
        [_valueLabel setContentCompressionResistancePriority:UILayoutPriorityDefaultLow
                                                     forAxis:UILayoutConstraintAxisHorizontal];

        UIView *spacer = [UIView new];
        UIStackView *s = [[UIStackView alloc] initWithArrangedSubviews:@[icon, k, spacer, _dot, _valueLabel]];
        s.spacing = 8;
        s.alignment = UIStackViewAlignmentCenter;
        s.translatesAutoresizingMaskIntoConstraints = NO;
        [self addSubview:s];
        [NSLayoutConstraint activateConstraints:@[
            [s.topAnchor constraintEqualToAnchor:self.topAnchor],
            [s.bottomAnchor constraintEqualToAnchor:self.bottomAnchor],
            [s.leadingAnchor constraintEqualToAnchor:self.leadingAnchor],
            [s.trailingAnchor constraintEqualToAnchor:self.trailingAnchor],
            [self.heightAnchor constraintGreaterThanOrEqualToConstant:26],
        ]];
    }
    return self;
}

- (void)setHealthy:(BOOL)healthy {
    _dot.hidden = NO;
    _dot.backgroundColor = healthy ? UIColor.systemGreenColor : UIColor.systemRedColor;
    _valueLabel.text = healthy ? @"正常" : @"异常";
}

@end
