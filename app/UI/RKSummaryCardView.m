// RKSummaryCardView.m — 说明见头文件。

#import "RKSummaryCardView.h"
#import "RKWidgets.h"
#import "../../common/RKProtocol.h"

@interface RKSummaryRow : UIView
@property (nonatomic, readonly) UILabel *valueLabel, *deviceLabel;
@property (nonatomic, readonly) RKCompareBar *bar;
@end

@implementation RKSummaryRow

- (instancetype)initWithTitle:(NSString *)title {
    if ((self = [super initWithFrame:CGRectZero])) {
        UILabel *name = [UILabel new];
        name.text = title;
        name.font = [UIFont systemFontOfSize:13 weight:UIFontWeightSemibold];
        name.textColor = [UIColor secondaryLabelColor];

        _valueLabel = [UILabel new];
        _valueLabel.attributedText = RKValueWithUnit(@"—", 15);
        _bar = [RKCompareBar new];
        _deviceLabel = [UILabel new];
        _deviceLabel.font = [UIFont monospacedDigitSystemFontOfSize:12 weight:UIFontWeightMedium];
        _deviceLabel.textColor = [UIColor secondaryLabelColor];
        _deviceLabel.textAlignment = NSTextAlignmentRight;
        _deviceLabel.text = @"整机 —";

        // 固定列宽，无论数值如何两行都对齐。
        for (UIView *v in @[name, _valueLabel, _bar, _deviceLabel]) {
            v.translatesAutoresizingMaskIntoConstraints = NO;
            [self addSubview:v];
        }
        [NSLayoutConstraint activateConstraints:@[
            [name.leadingAnchor constraintEqualToAnchor:self.leadingAnchor],
            [name.widthAnchor constraintEqualToConstant:36],
            [_valueLabel.leadingAnchor constraintEqualToAnchor:name.trailingAnchor constant:6],
            [_valueLabel.widthAnchor constraintEqualToConstant:66],
            [_bar.leadingAnchor constraintEqualToAnchor:_valueLabel.trailingAnchor constant:6],
            [_deviceLabel.leadingAnchor constraintEqualToAnchor:_bar.trailingAnchor constant:10],
            [_deviceLabel.widthAnchor constraintEqualToConstant:68],
            [_deviceLabel.trailingAnchor constraintEqualToAnchor:self.trailingAnchor],
            [self.heightAnchor constraintEqualToConstant:34],
        ]];
        for (UIView *v in @[name, _valueLabel, _bar, _deviceLabel]) {
            [v.centerYAnchor constraintEqualToAnchor:self.centerYAnchor].active = YES;
        }
    }
    return self;
}

@end

@implementation RKSummaryCardView {
    RKSummaryRow *_mem, *_cpu;
}

- (instancetype)initWithFrame:(CGRect)frame {
    if ((self = [super initWithFrame:frame])) {
        self.backgroundColor = [UIColor secondarySystemGroupedBackgroundColor];
        self.layer.cornerRadius = 22;
        self.layer.cornerCurve = kCACornerCurveContinuous;
        self.accessibilityLabel = @"性能监控";
        self.accessibilityTraits = UIAccessibilityTraitButton;

        UIImageView *icon = [[UIImageView alloc] initWithImage:RKSymbolImage(@"waveform.path.ecg", 15, UIFontWeightSemibold)];
        icon.tintColor = RKColorService();
        [icon setContentHuggingPriority:UILayoutPriorityRequired forAxis:UILayoutConstraintAxisHorizontal];
        UILabel *title = [UILabel new];
        title.text = @"性能监控";
        title.font = [UIFont systemFontOfSize:17 weight:UIFontWeightSemibold];
        UIImageView *chevron = [[UIImageView alloc] initWithImage:RKSymbolImage(@"chevron.right", 13, UIFontWeightSemibold)];
        chevron.tintColor = [UIColor tertiaryLabelColor];
        [chevron setContentHuggingPriority:UILayoutPriorityRequired forAxis:UILayoutConstraintAxisHorizontal];
        UIStackView *header = [[UIStackView alloc] initWithArrangedSubviews:@[icon, title, chevron]];
        header.spacing = 8;
        header.alignment = UIStackViewAlignmentCenter;

        _mem = [[RKSummaryRow alloc] initWithTitle:@"内存"];
        _cpu = [[RKSummaryRow alloc] initWithTitle:@"CPU"];

        UIStackView *col = [[UIStackView alloc] initWithArrangedSubviews:@[header, _mem, RKHairline(), _cpu]];
        col.axis = UILayoutConstraintAxisVertical;
        col.spacing = 2;
        [col setCustomSpacing:8 afterView:header];
        col.translatesAutoresizingMaskIntoConstraints = NO;
        // 纯展示内容：所有触摸都交给控件本身。
        col.userInteractionEnabled = NO;
        [self addSubview:col];
        [NSLayoutConstraint activateConstraints:@[
            [col.topAnchor constraintEqualToAnchor:self.topAnchor constant:16],
            [col.bottomAnchor constraintEqualToAnchor:self.bottomAnchor constant:-10],
            [col.leadingAnchor constraintEqualToAnchor:self.leadingAnchor constant:18],
            [col.trailingAnchor constraintEqualToAnchor:self.trailingAnchor constant:-18],
        ]];
    }
    return self;
}

- (void)setHighlighted:(BOOL)highlighted {
    [super setHighlighted:highlighted];
    [UIView animateWithDuration:highlighted ? 0.08 : 0.25 animations:^{
        self.transform = highlighted ? CGAffineTransformMakeScale(0.98, 0.98) : CGAffineTransformIdentity;
        self.alpha = highlighted ? 0.85 : 1;
    }];
}

- (void)applyMetrics:(NSDictionary *)m {
    if (!m) {
        for (RKSummaryRow *r in @[_mem, _cpu]) {
            r.valueLabel.attributedText = RKValueWithUnit(@"—", 15);
            r.deviceLabel.text = @"整机 —";
            [r.bar setService:0 device:0];
        }
        return;
    }
    double fp = [m[RKMetricServiceFootprint] doubleValue];
    double used = [m[RKMetricDeviceMemUsed] doubleValue];
    double total = [m[RKMetricDeviceMemTotal] doubleValue];
    _mem.valueLabel.attributedText = RKValueWithUnit(RKFormatBytes(fp), 15);
    _mem.deviceLabel.text = [@"整机 " stringByAppendingString:total > 0 ? RKFormatPercent(used / total * 100) : @"—"];
    [_mem.bar setService:total > 0 ? fp / total : 0 device:total > 0 ? used / total : 0];

    double cores = MAX(1, [m[RKMetricDeviceCores] doubleValue]);
    double svc = [m[RKMetricServiceCPU] doubleValue];    // 占单核的百分比
    double dev = [m[RKMetricDeviceCPU] doubleValue];     // 占整机的百分比
    _cpu.valueLabel.attributedText = RKValueWithUnit(RKFormatPercent(svc), 15);
    _cpu.deviceLabel.text = [@"整机 " stringByAppendingString:RKFormatPercent(dev)];
    [_cpu.bar setService:svc / cores / 100 device:dev / 100];
}

@end
