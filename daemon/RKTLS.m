// RKTLS.m — 说明见头文件。

#import "RKTLS.h"
#import <CommonCrypto/CommonDigest.h>

static NSString *RKHex(const unsigned char *md) {
    NSMutableString *s = [NSMutableString stringWithCapacity:64];
    for (int i = 0; i < CC_SHA256_DIGEST_LENGTH; i++) [s appendFormat:@"%02x", md[i]];
    return s;
}

NSString *RKSHA256Hex(NSData *data) {
    unsigned char md[CC_SHA256_DIGEST_LENGTH];
    CC_SHA256(data.bytes, (CC_LONG)data.length, md);
    return RKHex(md);
}

NSString *RKSHA256OfFile(NSString *path) {
    NSInputStream *in = [NSInputStream inputStreamWithFileAtPath:path];
    [in open];
    if (in.streamStatus != NSStreamStatusOpen) return nil;
    CC_SHA256_CTX ctx;
    CC_SHA256_Init(&ctx);
    uint8_t buf[16384];
    NSInteger n;
    while ((n = [in read:buf maxLength:sizeof(buf)]) > 0) CC_SHA256_Update(&ctx, buf, (CC_LONG)n);
    [in close];
    if (n < 0) return nil;
    unsigned char md[CC_SHA256_DIGEST_LENGTH];
    CC_SHA256_Final(md, &ctx);
    return RKHex(md);
}

NSString *RKNormalizePin(id pin) {
    if (![pin isKindOfClass:NSString.class]) return nil;
    NSString *hex = [[[pin stringByReplacingOccurrencesOfString:@":" withString:@""]
                      stringByTrimmingCharactersInSet:NSCharacterSet.whitespaceCharacterSet] lowercaseString];
    NSCharacterSet *bad = [[NSCharacterSet characterSetWithCharactersInString:@"0123456789abcdef"] invertedSet];
    return (hex.length == 64 && [hex rangeOfCharacterFromSet:bad].location == NSNotFound) ? hex : nil;
}

NSArray<NSString *> *RKNormalizePins(id pins) {
    NSArray *items = [pins isKindOfClass:NSArray.class] ? pins
        : [pins isKindOfClass:NSString.class] ? [pins componentsSeparatedByString:@","] : @[];
    NSMutableArray *out = [NSMutableArray array];
    for (id p in items) {
        NSString *n = RKNormalizePin(p);
        if (n && ![out containsObject:n]) [out addObject:n];
    }
    return out;
}

static SecCertificateRef RKLeaf(SecTrustRef trust) {
    if (@available(iOS 15.0, *)) {
        NSArray *chain = CFBridgingRelease(SecTrustCopyCertificateChain(trust));
        return chain.count ? (__bridge SecCertificateRef)chain[0] : NULL;   // 证书同时被 trust 持有，返回后仍有效
    }
    return SecTrustGetCertificateAtIndex(trust, 0);
}

NSString *RKLeafCertificatePin(SecTrustRef trust) {
    SecCertificateRef leaf = RKLeaf(trust);
    NSData *der = leaf ? CFBridgingRelease(SecCertificateCopyData(leaf)) : nil;
    return der ? RKSHA256Hex(der) : nil;
}

static NSString *RKLeafKeyPin(SecTrustRef trust) {
    SecCertificateRef leaf = RKLeaf(trust);
    if (!leaf) return nil;
    SecKeyRef key = SecCertificateCopyKey(leaf);
    if (!key) return nil;
    NSData *raw = CFBridgingRelease(SecKeyCopyExternalRepresentation(key, NULL));
    CFRelease(key);
    return raw ? RKSHA256Hex(raw) : nil;
}

BOOL RKTrustMatchesPins(SecTrustRef trust, NSArray<NSString *> *pins) {
    if (!trust || !pins.count) return NO;
    NSString *cert = RKLeafCertificatePin(trust);
    if (cert && [pins containsObject:cert]) return YES;
    NSString *key = RKLeafKeyPin(trust);
    return key && [pins containsObject:key];
}

BOOL RKTrustIsSystemValid(SecTrustRef trust, NSString *host) {
    if (!trust || !host.length) return NO;
    SecPolicyRef policy = SecPolicyCreateSSL(true, (__bridge CFStringRef)host);
    OSStatus st = SecTrustSetPolicies(trust, policy);
    CFRelease(policy);
    return st == errSecSuccess && SecTrustEvaluateWithError(trust, NULL);
}
