// RKScannerViewController — 扫码页：相机取景，识别到二维码后回调文本。
// onScan 返回 YES 表示已处理（页面随即关闭），NO 表示不是要的码，提示后继续扫。

#import <UIKit/UIKit.h>

NS_ASSUME_NONNULL_BEGIN

@interface RKScannerViewController : UIViewController
@property (nonatomic, copy) NSString *hint;   // 取景框下方的说明
@property (nonatomic, copy) BOOL (^onScan)(NSString *text);
@end

NS_ASSUME_NONNULL_END
