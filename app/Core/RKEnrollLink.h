// RKEnrollLink — 接入二维码：控制台「添加设备」生成，App 扫码或用系统相机扫码打开。
//
//   residentkit://enroll?u=<服务器地址>&t=<接入码>&p=<证书指纹>
//     u  wss:// 或 ws:// 地址（必填）
//     t  一次性接入码（可选，服务端核对后直接批准，不用在控制台点同意）
//     p  证书指纹，逗号分隔（可选，自签证书时带上，域名证书不需要）
//
// 也接受直接是 wss:// 地址的二维码（只设置服务器，照常申请接入）。

#import <UIKit/UIKit.h>

NS_ASSUME_NONNULL_BEGIN

extern NSString *const RKURLScheme;   // 打开 App 的 URL scheme，Info.plist 里注册同一个

@interface RKEnrollLink : NSObject
@property (nonatomic, readonly) NSString *serverURL;
@property (nonatomic, readonly) NSString *host;
@property (nonatomic, readonly, nullable) NSString *ticket;
@property (nonatomic, readonly, nullable) NSString *pin;

// 二维码文本或打开 App 的 URL；不是接入码返回 nil。
+ (nullable instancetype)linkWithString:(NSString *)string;

// 弹窗确认后写入连接配置并立即申请接入。presenter 为当前顶层页面。
- (void)confirmAndApplyFrom:(UIViewController *)presenter;
@end

NS_ASSUME_NONNULL_END
