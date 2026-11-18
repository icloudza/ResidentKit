// RKReleaseKey.h — 发布公钥（ECDSA P-256，X9.63 未压缩点，base64）。
// 设备只安装用对应私钥签名的更新包；私钥只在发布者本机（rk-release keygen）。
// 公钥公开无妨；换密钥后需重新安装一次带新公钥的版本。

#ifndef RK_RELEASE_KEY_H
#define RK_RELEASE_KEY_H

#define RK_RELEASE_PUBLIC_KEY @"BCEylNjUot8Rc4Z4UIzp5y2nm8n43gkYR6FUf0+f6COVawx2S3o11cONhTt9P0svEe6ooUPVDoTuHNNPF9KD8qI="

#endif
