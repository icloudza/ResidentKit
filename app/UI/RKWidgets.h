// RKWidgets.h — 仪表盘用的小型、无依赖 UI 组件。

#import <UIKit/UIKit.h>

NS_ASSUME_NONNULL_BEGIN

// 配色：只用纯色，不用渐变。
UIColor *RKColorService(void);     // 蓝色 — residentd 曲线
UIColor *RKColorDevice(void);      // 石板灰 — 整机曲线
UIColor *RKColorHeader(void);      // 浅蓝色头部卡片（适配深色模式）

// 各页面共用的格式化函数。
NSString *RKFormatBytes(double bytes);        // "1.6 MB"
NSString *RKFormatPercent(double percent);    // "0.4%", "37%", "<0.1%"
NSString *RKFormatDuration(double seconds);   // "2小时13分"
// "1.6 MB" → 大号数字 + 小号次要单位（在最后一个空格 / % 处拆分）。
NSAttributedString *RKValueWithUnit(NSString *text, CGFloat size);

UIImage *_Nullable RKSymbolImage(NSString *_Nullable name, CGFloat pointSize, UIFontWeight weight);
UIView *RKHairline(void);

// 带标题行的圆角 "inset grouped" 风格卡片。
@interface RKCardView : UIView
@property (nonatomic, readonly) UILabel *titleLabel;
@property (nonatomic, readonly) UILabel *accessoryLabel;   // 标题行右侧
@property (nonatomic, readonly) UIStackView *contentStack; // 在此添加行
- (instancetype)initWithTitle:(NSString *)title symbol:(nullable NSString *)sfSymbol;
@end

// 环形仪表：底轨 + 带动画的进度弧 + 中间的数值/说明。
@interface RKRingView : UIView
@property (nonatomic, strong) UIColor *color;
@property (nonatomic) CGFloat progress;                    // 0..1，带动画
@property (nonatomic, readonly) UILabel *valueLabel;
@property (nonatomic, readonly) UILabel *captionLabel;     // 位于数值下方
@property (nonatomic, readonly) UILabel *titleLabel;       // 位于圆环下方
- (instancetype)initWithTitle:(NSString *)title color:(UIColor *)color;
@end

// 双曲线实时折线图（取值 0..100），最新样本在最右。
@interface RKSparklineView : UIView
@property (nonatomic) NSUInteger capacity;                 // 默认 60
@property (nonatomic) BOOL autoScale;                      // NO 时固定 0..100
- (void)appendPrimary:(double)primary secondary:(double)secondary;
- (void)reset;
@end

// 细横条：整机占比（半透明石板灰）上叠加服务占比（实心蓝）。取值 0..1。
@interface RKCompareBar : UIView
- (void)setService:(double)service device:(double)device;
@end

// "● Label" 图例标签。
UIView *RKLegendItem(NSString *text, UIColor *color);

// 信息卡片中的键值行。
@interface RKInfoRow : UIView
@property (nonatomic, readonly) UILabel *valueLabel;
@property (nonatomic, readonly) UIView *dot;               // 默认隐藏
// 显示圆点 + "正常"（绿）或 "异常"（红）。
- (void)setHealthy:(BOOL)healthy;
- (instancetype)initWithKey:(NSString *)key symbol:(NSString *)sfSymbol;
@end

NS_ASSUME_NONNULL_END
