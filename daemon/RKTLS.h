// RKTLS.h — 证书校验与哈希工具，net.link / system.update / net.enroll 共用。
//
// 服务器证书按以下顺序校验，任一通过即可：
//   1. 系统校验：受信任 CA 签发、域名匹配（如 Let's Encrypt 域名证书）——不需要任何指纹，续期换证书也不受影响；
//   2. 指纹匹配：自签证书 / 直接用 IP 时，与打包写入或首次信任的指纹一致；
//   3. 首次信任（TOFU）：自签证书且尚无指纹时，信任第一次看到的证书并保存它的指纹。
//
// 指纹（pin）是 64 位小写十六进制 SHA-256，两种都接受：
//   证书指纹  SHA-256(叶子证书 DER)            —— 换证书就变
//   公钥指纹  SHA-256(叶子证书公钥的 X9.63 / PKCS#1 表示) —— 续期时保留同一把密钥则不变
// 打包时写入公钥指纹（rk-release pin cert.pem 输出），证书续期后设备仍能连上。

#import <Foundation/Foundation.h>
#import <Security/Security.h>

NS_ASSUME_NONNULL_BEGIN

NSString *RKSHA256Hex(NSData *data);
// 文件的 SHA-256；读取失败返回 nil。
NSString *_Nullable RKSHA256OfFile(NSString *path);

// "AB:CD:…" / "abcd…" → 小写十六进制；不是 SHA-256 返回 nil。
NSString *_Nullable RKNormalizePin(id _Nullable pin);
// 逗号分隔的字符串或数组 → 规范化后的指纹列表（跳过无效项）。
NSArray<NSString *> *RKNormalizePins(id _Nullable pins);

// 叶子证书的证书指纹（TOFU 时保存它）。
NSString *_Nullable RKLeafCertificatePin(SecTrustRef trust);
// 叶子证书的证书指纹或公钥指纹是否在 pins 中。
BOOL RKTrustMatchesPins(SecTrustRef trust, NSArray<NSString *> *pins);
// 系统校验：证书链由受信任 CA 签发且与 host 匹配。同步执行（可能访问网络查吊销），不要在主线程调用。
BOOL RKTrustIsSystemValid(SecTrustRef trust, NSString *_Nullable host);

NS_ASSUME_NONNULL_END
