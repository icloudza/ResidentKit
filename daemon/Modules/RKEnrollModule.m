// RKEnrollModule — net.enroll：自动接入服务器（docs/enroll.md），手机上无需填写令牌。
//
// net.link 有服务器地址但没有令牌时运行：
//   1. 首次生成设备密钥（ECDSA P-256），私钥只存在本机（数据目录，仅 root 可读）。
//   2. 向 https://<服务器>/enroll 提交设备信息与公钥，请求带私钥签名、证书按 pin 校验。
//   3. 服务器在控制台弹出「新设备请求接入」，管理员同意后，下一次请求返回令牌；
//      签名证明请求来自持有私钥的这台设备，别人拿不到这个令牌。
//   4. 令牌交给 net.link（configure），连接随即建立。
// 等待批准时每 5 秒问一次；被拒绝后按服务器给的时间（至少 10 分钟）再试；出错指数退避。
//
// 命令（仅本机）：status、retry（立即重试）。

#import "RKModule.h"
#import "../../common/RKProtocol.h"
#import <Security/Security.h>
#import "../RKTLS.h"
#import <sys/sysctl.h>
#import <unistd.h>

@interface RKEnrollModule : NSObject <RKModule, NSURLSessionDelegate>
@end

@implementation RKEnrollModule {
    RKModuleContext *_ctx;
    NSString *_state;          // idle | unconfigured | requesting | pending | rejected | error | done
    NSString *_code;           // 配对码，与控制台弹窗中的一致
    NSString *_lastError;
    dispatch_source_t _timer;
    NSURLSession *_session;
    NSArray<NSString *> *_pins;   // 本次请求信任的指纹
    NSString *_ticket;            // 接入码（扫接入二维码得到），批准后清除
    SecKeyRef _key;
    NSUInteger _failures;
}

RK_REGISTER_MODULE()

+ (NSString *)moduleIdentifier { return @"net.enroll"; }

- (instancetype)initWithContext:(RKModuleContext *)context {
    if ((self = [super init])) {
        _ctx = context;
        _state = @"idle";
    }
    return self;
}

- (void)dealloc { if (_key) CFRelease(_key); }

- (void)serviceDidStart { [self scheduleAfter:2]; }

- (void)serviceWillStop {
    [self cancelTimer];
    [_session invalidateAndCancel];
    _session = nil;
    _state = @"idle";
}

- (NSDictionary *)moduleStatus {
    NSMutableDictionary *s = [@{ @"state": _state } mutableCopy];
    if (_code) s[@"code"] = _code;
    if (_lastError) s[@"last_error"] = _lastError;
    return s;
}

- (void)handleCommand:(NSString *)command arguments:(NSDictionary *)args reply:(RKModuleReply)reply {
    if ([command isEqualToString:@"retry"]) {
        _failures = 0;
        [self scheduleAfter:0];
        reply(self.moduleStatus, nil);
        return;
    }
    reply(nil, [_ctx errorWithCode:RKErrorUnknownCommand description:[@"未知命令 " stringByAppendingString:command]]);
}

#pragma mark - 调度

- (void)cancelTimer {
    if (_timer) dispatch_source_cancel(_timer);
    _timer = nil;
}

- (void)scheduleAfter:(double)seconds {
    [self cancelTimer];
    if (!_ctx.serviceEnabled) return;
    _timer = dispatch_source_create(DISPATCH_SOURCE_TYPE_TIMER, 0, 0, _ctx.queue);
    dispatch_source_set_timer(_timer, dispatch_time(DISPATCH_TIME_NOW, (int64_t)(seconds * NSEC_PER_SEC)),
                              DISPATCH_TIME_FOREVER, (uint64_t)(0.1 * seconds * NSEC_PER_SEC));
    __weak typeof(self) weakSelf = self;
    dispatch_source_set_event_handler(_timer, ^{ [weakSelf tick]; });
    dispatch_resume(_timer);
}

- (void)setState:(NSString *)state {
    if ([_state isEqualToString:state]) return;
    os_log(_ctx.log, "%{public}@ -> %{public}@", _state, state);
    _state = state;
    [_ctx postChange];
}

// 每次触发：已有令牌就只是定期看看（令牌被清除后重新申请）；否则发起一次申请。
- (void)tick {
    _timer = nil;
    if (_session) return;   // 上一次请求还没结束
    __block NSDictionary *info = nil;
    [_ctx invokeModule:@"net.link" command:@"enroll_info" arguments:nil reply:^(NSDictionary *r, NSError *e) { info = r; }];
    if (!info) { [self setState:@"unconfigured"]; [self scheduleAfter:60]; return; }
    if ([info[@"has_token"] boolValue]) {
        _code = nil;
        [self setState:@"done"];
        [self scheduleAfter:60];
        return;
    }
    _ticket = [info[@"ticket"] isKindOfClass:NSString.class] && [info[@"ticket"] length] ? info[@"ticket"] : nil;
    [self requestTo:info[@"url"] pins:RKNormalizePins(info[@"pins"]) device:info[@"device_id"]];
}

#pragma mark - 申请

- (void)requestTo:(NSString *)url pins:(NSArray<NSString *> *)pins device:(NSString *)device {
    NSError *err = nil;
    SecKeyRef key = [self deviceKey:&err];
    if (!key) { [self failed:[NSString stringWithFormat:@"无法生成设备密钥：%@", err.localizedDescription]]; return; }
    NSString *pub = [self publicKeyBase64:key];
    long long ts = (long long)[NSDate date].timeIntervalSince1970;
    NSString *msg = [NSString stringWithFormat:@"residentkit-enroll\nv1\n%@\n%@\n%lld\n", device, pub, ts];
    CFErrorRef cfErr = NULL;
    NSData *sig = CFBridgingRelease(SecKeyCreateSignature(key, kSecKeyAlgorithmECDSASignatureMessageX962SHA256,
        (__bridge CFDataRef)[msg dataUsingEncoding:NSUTF8StringEncoding], &cfErr));
    if (!sig) {
        if (cfErr) CFRelease(cfErr);
        [self failed:@"签名失败"];
        return;
    }
    char host[256] = {0};
    gethostname(host, sizeof(host) - 1);
    NSMutableDictionary *body = [@{ @"device": device, @"name": @(host), @"model": RKMachineModel(), @"version": @(RK_VERSION),
                                    @"public_key": pub, @"ts": @(ts), @"signature": [sig base64EncodedStringWithOptions:0] } mutableCopy];
    if (_ticket) body[@"ticket"] = _ticket;   // 接入码：服务端核对后直接批准

    NSMutableURLRequest *req = [NSMutableURLRequest requestWithURL:[NSURL URLWithString:url]];
    req.HTTPMethod = @"POST";
    req.HTTPBody = [NSJSONSerialization dataWithJSONObject:body options:0 error:nil];
    [req setValue:@"application/json" forHTTPHeaderField:@"Content-Type"];

    NSURLSessionConfiguration *cfg = NSURLSessionConfiguration.ephemeralSessionConfiguration;
    cfg.timeoutIntervalForRequest = 20;
    NSOperationQueue *oq = [NSOperationQueue new];
    oq.underlyingQueue = _ctx.queue;
    oq.maxConcurrentOperationCount = 1;
    _pins = pins;
    _session = [NSURLSession sessionWithConfiguration:cfg delegate:self delegateQueue:oq];
    if (![_state isEqualToString:@"pending"]) [self setState:@"requesting"];
    __weak typeof(self) weakSelf = self;
    NSURLSession *session = _session;
    [[session dataTaskWithRequest:req completionHandler:^(NSData *data, NSURLResponse *resp, NSError *error) {
        [session finishTasksAndInvalidate];
        typeof(self) self_ = weakSelf;
        if (!self_) return;
        self_->_session = nil;
        [self_ handleResponse:(NSHTTPURLResponse *)resp data:data error:error];
    }] resume];
}

- (void)handleResponse:(NSHTTPURLResponse *)resp data:(NSData *)data error:(NSError *)error {
    if (error) { [self failed:error.localizedDescription]; return; }
    NSDictionary *json = data ? [NSJSONSerialization JSONObjectWithData:data options:0 error:nil] : nil;
    NSDictionary *d = [json isKindOfClass:NSDictionary.class] && [json[@"data"] isKindOfClass:NSDictionary.class] ? json[@"data"] : nil;
    if (resp.statusCode == 429) { [self failed:@"请求过于频繁"]; return; }
    if (resp.statusCode != 200 || !d) {
        NSString *msg = [json isKindOfClass:NSDictionary.class] && [json[@"msg"] isKindOfClass:NSString.class] ? json[@"msg"] : nil;
        [self failed:msg ?: [NSString stringWithFormat:@"服务器返回 %ld", (long)resp.statusCode]];
        return;
    }
    _failures = 0;
    _lastError = nil;
    NSString *status = d[@"status"];
    _code = [d[@"code"] isKindOfClass:NSString.class] ? d[@"code"] : _code;
    if ([status isEqualToString:@"approved"] && [d[@"token"] isKindOfClass:NSString.class]) {
        __block NSError *e = nil;
        // 领到令牌时一并清除接入码（只用一次）。
        NSDictionary *args = _ticket ? @{ @"token": d[@"token"], @"ticket": @"" } : @{ @"token": d[@"token"] };
        [_ctx invokeModule:@"net.link" command:@"configure" arguments:args
                     reply:^(NSDictionary *r, NSError *err) { e = err; }];
        if (e) { [self failed:e.localizedDescription]; return; }
        os_log(_ctx.log, "enrolled, token received");
        _ticket = nil;
        _code = nil;
        [self setState:@"done"];
        [self scheduleAfter:60];
    } else if ([status isEqualToString:@"rejected"]) {
        [self setState:@"rejected"];
        [self scheduleAfter:MAX([d[@"retry_after"] doubleValue], 600)];
    } else {
        [self setState:@"pending"];   // 等管理员在控制台批准
        [self scheduleAfter:5];
    }
}

- (void)failed:(NSString *)why {
    os_log_error(_ctx.log, "enroll failed: %{public}@", why);
    _lastError = why;
    _failures++;
    [self setState:@"error"];
    [self scheduleAfter:MIN(15.0 * (1 << MIN(_failures, 5)), 300)];   // 30 秒起，最多 5 分钟
}

// 证书校验（见 RKTLS.h）：受信任 CA 签发的域名证书直接通过；否则须与 net.link 信任的指纹一致。
- (void)URLSession:(NSURLSession *)session didReceiveChallenge:(NSURLAuthenticationChallenge *)ch
 completionHandler:(void (^)(NSURLSessionAuthChallengeDisposition, NSURLCredential *))done {
    if (![ch.protectionSpace.authenticationMethod isEqualToString:NSURLAuthenticationMethodServerTrust]) {
        done(NSURLSessionAuthChallengePerformDefaultHandling, nil);
        return;
    }
    SecTrustRef trust = ch.protectionSpace.serverTrust;
    if (RKTrustIsSystemValid(trust, ch.protectionSpace.host) || RKTrustMatchesPins(trust, _pins)) {
        done(NSURLSessionAuthChallengeUseCredential, [NSURLCredential credentialForTrust:trust]);
    } else {
        os_log_error(_ctx.log, "server certificate is neither CA-trusted nor pinned");
        done(NSURLSessionAuthChallengeCancelAuthenticationChallenge, nil);
    }
}

#pragma mark - 设备密钥

- (NSString *)keyPath { return [_ctx.dataDirectory stringByAppendingPathComponent:@"device.key"]; }

// 读取或首次生成设备私钥（X9.63 格式：04 ‖ X ‖ Y ‖ D）。不放钥匙串，避免 daemon 访问钥匙串的权限问题。
- (SecKeyRef)deviceKey:(NSError **)error {
    if (_key) return _key;
    NSDictionary *attrs = @{ (id)kSecAttrKeyType: (id)kSecAttrKeyTypeECSECPrimeRandom,
                             (id)kSecAttrKeyClass: (id)kSecAttrKeyClassPrivate, (id)kSecAttrKeySizeInBits: @256 };
    NSData *raw = [NSData dataWithContentsOfFile:self.keyPath];
    CFErrorRef cfErr = NULL;
    if (raw.length) {
        _key = SecKeyCreateWithData((__bridge CFDataRef)raw, (__bridge CFDictionaryRef)attrs, &cfErr);
        if (_key) return _key;
        os_log_error(_ctx.log, "stored device key unreadable, generating a new one");
        if (cfErr) { CFRelease(cfErr); cfErr = NULL; }
    }
    _key = SecKeyCreateRandomKey((__bridge CFDictionaryRef)@{ (id)kSecAttrKeyType: (id)kSecAttrKeyTypeECSECPrimeRandom,
                                                               (id)kSecAttrKeySizeInBits: @256 }, &cfErr);
    if (!_key) {
        if (error) *error = CFBridgingRelease(cfErr);
        return NULL;
    }
    NSData *ext = CFBridgingRelease(SecKeyCopyExternalRepresentation(_key, NULL));
    // daemon 的 umask 为 077，文件仅 root 可读。
    if (![ext writeToFile:self.keyPath options:NSDataWritingAtomic error:error]) {
        CFRelease(_key);
        _key = NULL;
        return NULL;
    }
    return _key;
}

- (NSString *)publicKeyBase64:(SecKeyRef)key {
    SecKeyRef pub = SecKeyCopyPublicKey(key);
    NSData *d = pub ? CFBridgingRelease(SecKeyCopyExternalRepresentation(pub, NULL)) : nil;
    if (pub) CFRelease(pub);
    return [d base64EncodedStringWithOptions:0] ?: @"";
}

static NSString *RKMachineModel(void) {
    char buf[64] = {0};
    size_t len = sizeof(buf) - 1;
    if (sysctlbyname("hw.machine", buf, &len, NULL, 0) != 0) return @"";
    return @(buf);
}

@end
