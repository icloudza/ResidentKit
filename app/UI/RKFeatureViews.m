// RKFeatureViews.m — 说明见头文件。

#import "RKFeatureViews.h"
#import "RKWidgets.h"

static UIView *RKFeatureIcon(NSString *symbol, CGFloat side, CGFloat pointSize) {
    UIView *box = [UIView new];
    box.backgroundColor = RKColorHeader();
    box.layer.cornerRadius = side * 0.29;
    box.layer.cornerCurve = kCACornerCurveContinuous;
    UIImageView *img = [[UIImageView alloc] initWithImage:RKSymbolImage(symbol, pointSize, UIFontWeightSemibold)];
    img.tintColor = RKColorService();
    img.contentMode = UIViewContentModeCenter;
    img.translatesAutoresizingMaskIntoConstraints = NO;
    [box addSubview:img];
    box.translatesAutoresizingMaskIntoConstraints = NO;
    [NSLayoutConstraint activateConstraints:@[
        [box.widthAnchor constraintEqualToConstant:side],
        [box.heightAnchor constraintEqualToConstant:side],
        [img.centerXAnchor constraintEqualToAnchor:box.centerXAnchor],
        [img.centerYAnchor constraintEqualToAnchor:box.centerYAnchor],
    ]];
    box.userInteractionEnabled = NO;
    return box;
}

@implementation RKFeatureView {
@protected
    UILabel *_title, *_subtitle;
}

- (instancetype)initWithFeature:(RKFeature *)feature {
    if ((self = [super initWithFrame:CGRectZero])) {
        _feature = feature;
        _title = [UILabel new];
        _title.font = [UIFont systemFontOfSize:15 weight:UIFontWeightSemibold];
        _subtitle = [UILabel new];
        _subtitle.font = [UIFont systemFontOfSize:12];
        _subtitle.textColor = [UIColor secondaryLabelColor];
        self.accessibilityTraits = UIAccessibilityTraitButton;
        self.isAccessibilityElement = YES;
    }
    return self;
}

- (BOOL)tappable { return self.feature.available && self.feature.selectable; }

- (void)update {
    _title.text = self.feature.title;
    _subtitle.text = self.feature.subtitle;
    _subtitle.hidden = _subtitle.text.length == 0;
    self.alpha = self.feature.available ? 1 : 0.45;
    self.accessibilityLabel = self.feature.title;
    self.accessibilityValue = self.feature.subtitle;
}

- (void)setHighlighted:(BOOL)highlighted {
    [super setHighlighted:highlighted];
    if (highlighted && ![self tappable]) return;
    [UIView animateWithDuration:highlighted ? 0.08 : 0.25 animations:^{
        self.transform = highlighted ? CGAffineTransformMakeScale(0.97, 0.97) : CGAffineTransformIdentity;
    }];
}

@end

#pragma mark - 宫格
@implementation RKFeatureTileView

- (instancetype)initWithFeature:(RKFeature *)feature {
    if ((self = [super initWithFeature:feature])) {
        self.backgroundColor = [UIColor secondarySystemGroupedBackgroundColor];
        self.layer.cornerRadius = 20;
        self.layer.cornerCurve = kCACornerCurveContinuous;

        UIView *icon = RKFeatureIcon(feature.symbolName, 34, 16);
        UIStackView *text = [[UIStackView alloc] initWithArrangedSubviews:@[_title, _subtitle]];
        text.axis = UILayoutConstraintAxisVertical;
        text.spacing = 2;
        UIStackView *col = [[UIStackView alloc] initWithArrangedSubviews:@[icon, [UIView new], text]];
        col.axis = UILayoutConstraintAxisVertical;
        col.alignment = UIStackViewAlignmentLeading;
        col.userInteractionEnabled = NO;
        col.translatesAutoresizingMaskIntoConstraints = NO;
        [self addSubview:col];
        [NSLayoutConstraint activateConstraints:@[
            [col.topAnchor constraintEqualToAnchor:self.topAnchor constant:14],
            [col.bottomAnchor constraintEqualToAnchor:self.bottomAnchor constant:-14],
            [col.leadingAnchor constraintEqualToAnchor:self.leadingAnchor constant:14],
            [col.trailingAnchor constraintEqualToAnchor:self.trailingAnchor constant:-14],
            [self.heightAnchor constraintGreaterThanOrEqualToConstant:104],
            [text.widthAnchor constraintEqualToAnchor:col.widthAnchor],
        ]];
        [self update];
    }
    return self;
}

@end

#pragma mark - 列表行
@implementation RKFeatureRowView {
    UISwitch *_switch;
    UIImageView *_chevron;
}

- (instancetype)initWithFeature:(RKFeature *)feature {
    if ((self = [super initWithFeature:feature])) {
        UIView *icon = RKFeatureIcon(feature.symbolName, 30, 14);
        UIStackView *text = [[UIStackView alloc] initWithArrangedSubviews:@[_title, _subtitle]];
        text.axis = UILayoutConstraintAxisVertical;
        text.spacing = 1;
        text.userInteractionEnabled = NO;
        text.translatesAutoresizingMaskIntoConstraints = NO;

        _switch = [UISwitch new];
        _switch.onTintColor = RKColorService();
        [_switch addTarget:self action:@selector(switchChanged:) forControlEvents:UIControlEventValueChanged];
        _chevron = [[UIImageView alloc] initWithImage:RKSymbolImage(@"chevron.right", 13, UIFontWeightSemibold)];
        _chevron.tintColor = [UIColor tertiaryLabelColor];
        _chevron.userInteractionEnabled = NO;

        // 不用外层 stack：只有开关接收触摸，其余都交给控件本身（整行点击）。
        for (UIView *v in @[icon, text, _switch, _chevron]) {
            v.translatesAutoresizingMaskIntoConstraints = NO;
            [self addSubview:v];
        }
        [NSLayoutConstraint activateConstraints:@[
            [icon.leadingAnchor constraintEqualToAnchor:self.leadingAnchor constant:16],
            [icon.centerYAnchor constraintEqualToAnchor:self.centerYAnchor],
            [text.leadingAnchor constraintEqualToAnchor:icon.trailingAnchor constant:12],
            [text.centerYAnchor constraintEqualToAnchor:self.centerYAnchor],
            [text.trailingAnchor constraintLessThanOrEqualToAnchor:_switch.leadingAnchor constant:-8],
            [_switch.trailingAnchor constraintEqualToAnchor:self.trailingAnchor constant:-16],
            [_switch.centerYAnchor constraintEqualToAnchor:self.centerYAnchor],
            [_chevron.trailingAnchor constraintEqualToAnchor:self.trailingAnchor constant:-16],
            [_chevron.centerYAnchor constraintEqualToAnchor:self.centerYAnchor],
            [self.heightAnchor constraintGreaterThanOrEqualToConstant:60],
            [text.topAnchor constraintGreaterThanOrEqualToAnchor:self.topAnchor constant:10],
        ]];
        [self update];
    }
    return self;
}

- (void)update {
    [super update];
    BOOL sw = self.feature.showsSwitch;
    _switch.hidden = !sw;
    _chevron.hidden = sw || !self.feature.selectable;
    _switch.enabled = self.feature.available;
    if (sw && _switch.on != self.feature.isOn) [_switch setOn:self.feature.isOn animated:YES];
}

- (void)switchChanged:(UISwitch *)s {
    [[[UIImpactFeedbackGenerator alloc] initWithStyle:UIImpactFeedbackStyleLight] impactOccurred];
    [self.feature switchDidChange:s.on];
}

@end
