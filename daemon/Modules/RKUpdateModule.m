// RKUpdateModule — system.update：OTA 整包更新（设备端）。
//
// install 流程：版本必须高于当前 → 本机没有当前版本的安装包时先从服务器取一份（供回滚）→
// 经 net.link 的地址 / 令牌 / 证书指纹下载新版本 → 校验大小、SHA-256、ECDSA 签名（公钥见 RKReleaseKey.h）
// → 写任务文件，以一次性 launchd 任务（launchctl bootstrap）交给独立的 rk-updater 安装。
// daemon 不能自己跑 dpkg：旧包的 prerm 会 bootout 掉 daemon 及其子进程。
// rk-updater 负责安装、健康检查与回滚，结果写入数据目录并发 Darwin 通知；
// 本模块（新旧版本皆可）读取结果，上报 system.update.* 事件。
//
// 远程命令：status、install {release_id, version, size, sha256, signature}

#import "RKModule.h"
#import "RKSystem.h"
#import "../../common/RKProtocol.h"
#import "../../common/RKReleaseKey.h"
#import "../RKTLS.h"
#import <Security/Security.h>
#import <notify.h>
#import <spawn.h>
#import <sys/wait.h>

extern char **environ;

static NSString *const RKUpdaterLabel = @"com.icloudza.residentd.updater";
static const char *RKUpdateResultNote = "com.icloudza.residentd.update-result";
static const char *RKUpdatedNote = "com.icloudza.residentd.updated";      // 通知 App：已更新
static const char *RKRelaunchNote = "com.icloudza.residentkit.relaunch";  // App 请求重新打开

static const double RKInstallTimeout = 600;   // 秒：交给 rk-updater 后迟迟没有结果则复位

static BOOL RKIsVersion(NSString *v) {
    static NSRegularExpression *re;
    static dispatch_once_t once;
    dispatch_once(&once, ^{ re = [NSRegularExpression regularExpressionWithPattern:@"^[0-9]{1,4}\\.[0-9]{1,4}\\.[0-9]{1,4}$" options:0 error:nil]; });
    return [v isKindOfClass:NSString.class] && [re firstMatchInString:v options:0 range:NSMakeRange(0, v.length)] != nil;
}

static int RKCompareVersions(NSString *a, NSString *b) {
    NSArray *pa = [a componentsSeparatedByString:@"."], *pb = [b componentsSeparatedByString:@"."];
    for (NSUInteger i = 0; i < 3; i++) {
        NSInteger x = i < pa.count ? [pa[i] integerValue] : 0, y = i < pb.count ? [pb[i] integerValue] : 0;
        if (x != y) return x < y ? -1 : 1;
    }
    return 0;
}

// 运行命令并等待退出，返回退出码（-1 表示无法启动）。
static int RKRun(NSArray<NSString *> *argv) {
    char **args = calloc(argv.count + 1, sizeof(char *));
    for (NSUInteger i = 0; i < argv.count; i++) args[i] = (char *)argv[i].UTF8String;
    pid_t pid;
    int rc = posix_spawn(&pid, args[0], NULL, NULL, args, environ);
    free(args);
    if (rc != 0) return -1;
    int status = 0;
    waitpid(pid, &status, 0);
    return WIFEXITED(status) ? WEXITSTATUS(status) : -1;
}

@interface RKUpdateModule : NSObject <RKModule, NSURLSessionDownloadDelegate>
@end

@implementation RKUpdateModule {
    RKModuleContext *_ctx;
    NSString *_state;          // idle / downloading / verifying / installing
    NSDictionary *_job;        // 当前任务（install 参数）
    NSArray<NSString *> *_pins;
    NSString *_baseURL, *_token;
    NSString *_phase;          // 下载阶段：current（取当前版本供回滚）/ new（新版本）
    NSURLSession *_session;
    dispatch_source_t _installTimer;
    NSString *_lastError;
    int _resultToken, _relaunchToken;
}

RK_REGISTER_MODULE()

+ (NSString *)moduleIdentifier { return @"system.update"; }
+ (NSArray<NSString *> *)remoteCommands { return @[ @"status", @"install" ]; }

+ (NSDictionary<NSString *, NSDictionary *> *)commandSchema {
    return @{
        @"status": @{ @"label": @"更新状态", @"desc": @"当前版本、进行中的更新与是否可回滚",
                      @"result": @[ @{ @"key": @"version", @"type": @"string", @"desc": @"当前版本" },
                                    @{ @"key": @"state", @"type": @"string", @"desc": @"idle · downloading · verifying · installing" },
                                    @{ @"key": @"target_version", @"type": @"string", @"desc": @"正在更新到的版本" },
                                    @{ @"key": @"rollback_available", @"type": @"bool", @"desc": @"本机是否有可回滚的版本" },
                                    @{ @"key": @"last_error", @"type": @"string", @"desc": @"最近一次失败原因" } ] },
        @"install": @{ @"label": @"安装更新", @"desc": @"下载并安装指定版本（由「发布」页的灰度计划下发）",
                       @"params": @[ @{ @"key": @"release_id", @"type": @"int", @"label": @"版本 ID", @"required": @YES },
                                     @{ @"key": @"version", @"type": @"string", @"label": @"版本号", @"required": @YES },
                                     @{ @"key": @"size", @"type": @"int", @"label": @"大小", @"required": @YES },
                                     @{ @"key": @"sha256", @"type": @"string", @"label": @"SHA-256", @"required": @YES },
                                     @{ @"key": @"signature", @"type": @"string", @"label": @"签名", @"required": @YES } ],
                       @"result": @[ @{ @"key": @"accepted", @"type": @"bool", @"desc": @"已接受，开始下载" } ] },
    };
}

- (instancetype)initWithContext:(RKModuleContext *)context {
    if ((self = [super init])) {
        _ctx = context;
        _state = @"idle";
        __weak typeof(self) weakSelf = self;
        dispatch_queue_t q = context.queue;
        notify_register_dispatch(RKUpdateResultNote, &_resultToken, q, ^(int t) { [weakSelf consumeResult]; });
        notify_register_dispatch(RKRelaunchNote, &_relaunchToken, q, ^(int t) { [weakSelf relaunchApp]; });
        // 启动时处理上一次安装留下的结果（新版本或回滚后的旧版本都会走到这里）。
        dispatch_async(q, ^{ [weakSelf consumeResult]; });
    }
    return self;
}

- (NSString *)dir { return _ctx.dataDirectory; }

- (NSDictionary *)moduleStatus {
    NSMutableDictionary *s = [@{ @"version": @(RK_VERSION), @"state": _state,
                                 @"rollback_available": @([[NSFileManager defaultManager]
                                     fileExistsAtPath:[self.dir stringByAppendingPathComponent:@"current.deb"]]) } mutableCopy];
    if (_job) s[@"target_version"] = _job[@"version"];
    if (_lastError) s[@"last_error"] = _lastError;
    return s;
}

- (void)handleCommand:(NSString *)command arguments:(NSDictionary *)args reply:(RKModuleReply)reply {
    if (![command isEqualToString:@"install"]) {
        reply(nil, [_ctx errorWithCode:RKErrorUnknownCommand description:[@"未知命令 " stringByAppendingString:command]]);
        return;
    }
    NSString *version = args[@"version"], *sha = args[@"sha256"], *sig = args[@"signature"];
    NSNumber *size = args[@"size"], *rid = args[@"release_id"];
    if (![version isKindOfClass:NSString.class] || ![sha isKindOfClass:NSString.class] || ![sig isKindOfClass:NSString.class] ||
        ![size isKindOfClass:NSNumber.class] || ![rid isKindOfClass:NSNumber.class] || size.longLongValue <= 0 ||
        size.longLongValue > 64 << 20) {
        reply(nil, [_ctx errorWithCode:RKErrorInvalidArguments description:@"install 参数不完整"]);
        return;
    }
    if (!RKIsVersion(version)) {
        reply(nil, [_ctx errorWithCode:RKErrorInvalidArguments description:@"版本号格式应为 x.y.z"]);
        return;
    }
    if (RKCompareVersions(version, @(RK_VERSION)) <= 0) {
        reply(nil, [_ctx errorWithCode:RKErrorInvalidArguments
                           description:[NSString stringWithFormat:@"%@ 不高于当前版本 %s，拒绝降级", version, RK_VERSION]]);
        return;
    }
    if (![_state isEqualToString:@"idle"]) {
        reply(nil, [_ctx errorWithCode:RKErrorModuleFailed description:@"已有更新在进行中"]);
        return;
    }
    // 下载地址、令牌、证书指纹取自 net.link（不经服务器下发，服务器无法把设备引向别处）。
    __block NSDictionary *info = nil;
    [_ctx invokeModule:@"net.link" command:@"download_info" arguments:nil reply:^(NSDictionary *r, NSError *e) { info = r; }];
    if (!info) {
        reply(nil, [_ctx errorWithCode:RKErrorModuleFailed description:@"服务器连接未配置"]);
        return;
    }
    _job = @{ @"release_id": rid, @"version": version, @"size": size, @"sha256": sha.lowercaseString, @"signature": sig };
    _pins = RKNormalizePins(info[@"pins"]);
    _baseURL = info[@"base_url"];
    _token = info[@"token"];
    _lastError = nil;
    reply(@{ @"accepted": @YES, @"version": version }, nil);
    [_ctx emitEvent:@"system.update.started" level:@"info" payload:@{ @"release_id": rid, @"version": version, @"from": @(RK_VERSION) }];
    _state = @"downloading";
    [_ctx postChange];
    if (self.hasRollbackPackage) [self downloadNew];
    else [self download:@"current" from:[NSString stringWithFormat:@"%@version/%s", _baseURL, RK_VERSION]];
}

// 本机缓存的回滚包（current.deb）确实是当前运行的版本。
- (BOOL)hasRollbackPackage {
    NSString *v = [NSString stringWithContentsOfFile:[self.dir stringByAppendingPathComponent:@"current.version"]
                                            encoding:NSUTF8StringEncoding error:nil];
    return [[v stringByTrimmingCharactersInSet:NSCharacterSet.whitespaceAndNewlineCharacterSet] isEqualToString:@(RK_VERSION)] &&
           [[NSFileManager defaultManager] fileExistsAtPath:[self.dir stringByAppendingPathComponent:@"current.deb"]];
}

- (void)downloadNew {
    [self download:@"new" from:[NSString stringWithFormat:@"%@%@", _baseURL, _job[@"release_id"]]];
}

#pragma mark - 下载

- (void)download:(NSString *)phase from:(NSString *)url {
    _phase = phase;
    NSURLSessionConfiguration *cfg = NSURLSessionConfiguration.ephemeralSessionConfiguration;
    cfg.timeoutIntervalForRequest = 60;
    cfg.timeoutIntervalForResource = 600;
    NSOperationQueue *oq = [NSOperationQueue new];
    oq.underlyingQueue = _ctx.queue;   // 回调在模块队列上
    oq.maxConcurrentOperationCount = 1;
    _session = [NSURLSession sessionWithConfiguration:cfg delegate:self delegateQueue:oq];
    NSMutableURLRequest *req = [NSMutableURLRequest requestWithURL:[NSURL URLWithString:url]];
    [req setValue:[@"Bearer " stringByAppendingString:_token ?: @""] forHTTPHeaderField:@"Authorization"];
    [[_session downloadTaskWithRequest:req] resume];
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
        done(NSURLSessionAuthChallengeCancelAuthenticationChallenge, nil);
    }
}

- (void)URLSession:(NSURLSession *)session downloadTask:(NSURLSessionDownloadTask *)task didFinishDownloadingToURL:(NSURL *)location {
    if (session != _session) return;
    NSHTTPURLResponse *resp = (NSHTTPURLResponse *)task.response;
    if ([_phase isEqualToString:@"current"]) {
        [self cacheCurrent:location response:resp];
        [self finishSession:session];
        [self downloadNew];   // 回滚包取不到也继续更新，只是失败时无法回滚
        return;
    }
    if (resp.statusCode != 200) {
        [self finishSession:session];
        [self fail:@"download" error:[NSString stringWithFormat:@"服务器返回 %ld", (long)resp.statusCode]];
        return;
    }
    // 回调返回后临时文件即被删除，必须在这里同步移走。
    NSString *dest = [self.dir stringByAppendingPathComponent:@"incoming.deb"];
    [[NSFileManager defaultManager] removeItemAtPath:dest error:nil];
    NSError *err = nil;
    if (![[NSFileManager defaultManager] moveItemAtURL:location toURL:[NSURL fileURLWithPath:dest] error:&err]) {
        [self finishSession:session];
        [self fail:@"download" error:err.localizedDescription ?: @"保存失败"];
        return;
    }
    [self finishSession:session];
    [self verifyAndInstall:dest];
}

- (void)URLSession:(NSURLSession *)session task:(NSURLSessionTask *)task didCompleteWithError:(NSError *)error {
    if (session != _session) return;   // 已在下载完成回调里处理过
    [self finishSession:session];
    if (!error) return;
    if ([_phase isEqualToString:@"current"]) {
        os_log_error(_ctx.log, "rollback package unavailable: %{public}@", error.localizedDescription);
        [self downloadNew];
    } else if ([_state isEqualToString:@"downloading"]) {
        [self fail:@"download" error:error.localizedDescription];
    }
}

- (void)finishSession:(NSURLSession *)session {
    [session finishTasksAndInvalidate];
    if (session == _session) _session = nil;
}

// 保存当前版本的安装包作为回滚包；服务器随包返回签名清单（X-RK-*），按发布签名校验。
- (void)cacheCurrent:(NSURL *)location response:(NSHTTPURLResponse *)resp {
    NSString *why = nil;
    long long size = [[resp valueForHTTPHeaderField:@"X-RK-Size"] longLongValue];
    NSString *sha = [[resp valueForHTTPHeaderField:@"X-RK-SHA256"] lowercaseString];
    NSString *sig = [resp valueForHTTPHeaderField:@"X-RK-Signature"];
    NSDictionary *attrs = [[NSFileManager defaultManager] attributesOfItemAtPath:location.path error:nil];
    if (resp.statusCode != 200) why = [NSString stringWithFormat:@"服务器返回 %ld", (long)resp.statusCode];
    else if (size <= 0 || [attrs[NSFileSize] longLongValue] != size) why = @"大小不符";
    else if (![RKSHA256OfFile(location.path) isEqualToString:sha]) why = @"SHA-256 不符";
    else if (![self verifySignature:sig message:[self releaseMessage:@(RK_VERSION) size:size sha256:sha]]) why = @"签名无效";
    if (why) {
        os_log_error(_ctx.log, "rollback package %s not cached: %{public}@", RK_VERSION, why);
        return;
    }
    NSString *dest = [self.dir stringByAppendingPathComponent:@"current.deb"];
    [[NSFileManager defaultManager] removeItemAtPath:dest error:nil];
    if ([[NSFileManager defaultManager] moveItemAtURL:location toURL:[NSURL fileURLWithPath:dest] error:nil]) {
        [@(RK_VERSION) writeToFile:[self.dir stringByAppendingPathComponent:@"current.version"] atomically:YES
                          encoding:NSUTF8StringEncoding error:nil];
        os_log(_ctx.log, "cached rollback package %s", RK_VERSION);
    }
}

- (NSData *)releaseMessage:(NSString *)version size:(long long)size sha256:(NSString *)sha {
    return [[NSString stringWithFormat:@"residentkit-release\nv1\n%@\n%@\n%lld\n%@\n",
             @"com.icloudza.residentkit", version, size, sha] dataUsingEncoding:NSUTF8StringEncoding];
}

#pragma mark - 校验与安装

- (void)verifyAndInstall:(NSString *)path {
    _state = @"verifying";
    NSDictionary *job = _job;
    NSDictionary *attrs = [[NSFileManager defaultManager] attributesOfItemAtPath:path error:nil];
    if ([attrs[NSFileSize] longLongValue] != [job[@"size"] longLongValue]) {
        [self fail:@"verify" error:@"大小与清单不符"];
        return;
    }
    NSString *sum = RKSHA256OfFile(path);
    if (![sum isEqualToString:job[@"sha256"]]) {
        [self fail:@"verify" error:@"SHA-256 与清单不符"];
        return;
    }
    if (![self verifySignature:job[@"signature"] message:[self releaseMessage:job[@"version"] size:[job[@"size"] longLongValue] sha256:sum]]) {
        [self fail:@"verify" error:@"签名校验失败"];
        return;
    }

    // dpkg 依赖 /var/jb 链接，「隐藏越狱」期间装不了：明确报错，取消隐藏后在控制台重试即可。
    if (RKJailbreakHidden()) {
        [self fail:@"install" error:@"越狱已隐藏，取消隐藏后再更新"];
        return;
    }
    // 交给 rk-updater：写任务文件，以独立的一次性 launchd 任务启动（iOS 的 launchd 不支持 submit）。
    _state = @"installing";
    [_ctx postChange];
    NSString *deb = [self.dir stringByAppendingPathComponent:[NSString stringWithFormat:@"%@.deb", job[@"version"]]];
    [[NSFileManager defaultManager] removeItemAtPath:deb error:nil];
    if (![[NSFileManager defaultManager] moveItemAtPath:path toPath:deb error:nil]) {
        [self fail:@"install" error:@"无法保存安装包"];
        return;
    }
    NSDictionary *task = @{ @"deb": deb, @"version": job[@"version"], @"release_id": job[@"release_id"],
                            @"from": @(RK_VERSION), @"dir": self.dir };
    NSString *jobPath = [self.dir stringByAppendingPathComponent:@"job.plist"];
    if (![task writeToFile:jobPath atomically:YES]) {
        [self fail:@"install" error:@"无法写入任务文件"];
        return;
    }
    NSString *root = RKJailbreakRoot();
    NSString *launchctl = [root stringByAppendingString:@"/usr/bin/launchctl"];
    NSDictionary *plist = @{
        @"Label": RKUpdaterLabel,
        @"ProgramArguments": @[ [root stringByAppendingString:@"/usr/libexec/rk-updater"], jobPath ],
        @"EnvironmentVariables": @{ @"JBROOT": root },
        @"RunAtLoad": @YES,
        @"LaunchOnlyOnce": @YES,   // 退出后不再拉起，launchd 自动卸载
    };
    NSString *plistPath = [self.dir stringByAppendingPathComponent:@"updater.plist"];
    if (![plist writeToFile:plistPath atomically:YES]) {
        [self fail:@"install" error:@"无法写入任务文件"];
        return;
    }
    RKRun(@[ launchctl, @"bootout", [@"system/" stringByAppendingString:RKUpdaterLabel] ]);   // 清理残留，不存在时报错，忽略
    int rc = RKRun(@[ launchctl, @"bootstrap", @"system", plistPath ]);
    if (rc != 0) {
        [self fail:@"install" error:[NSString stringWithFormat:@"无法启动更新程序（%d）", rc]];
        return;
    }
    // 成功的话，接下来 rk-updater 会通过 dpkg 重启 daemon，本进程随之结束。
    // 若更新程序中途异常退出、一直没有结果，超时后复位，避免之后的更新都被「进行中」挡住。
    __weak typeof(self) weakSelf = self;
    _installTimer = dispatch_source_create(DISPATCH_SOURCE_TYPE_TIMER, 0, 0, _ctx.queue);
    dispatch_source_set_timer(_installTimer, dispatch_time(DISPATCH_TIME_NOW, (int64_t)(RKInstallTimeout * NSEC_PER_SEC)),
                              DISPATCH_TIME_FOREVER, 10 * NSEC_PER_SEC);
    dispatch_source_set_event_handler(_installTimer, ^{
        typeof(self) self_ = weakSelf;
        if (self_ && [self_->_state isEqualToString:@"installing"])
            [self_ fail:@"install" error:@"更新程序未在 10 分钟内返回结果"];
    });
    dispatch_resume(_installTimer);
}

- (void)fail:(NSString *)stage error:(NSString *)error {
    os_log_error(_ctx.log, "update failed at %{public}@: %{public}@", stage, error);
    if (_installTimer) dispatch_source_cancel(_installTimer);
    _installTimer = nil;
    NSDictionary *job = _job;
    _lastError = error;
    _state = @"idle";
    _job = nil;
    [[NSFileManager defaultManager] removeItemAtPath:[self.dir stringByAppendingPathComponent:@"incoming.deb"] error:nil];
    [_ctx emitEvent:@"system.update.failed" level:@"error"
            payload:@{ @"release_id": job[@"release_id"] ?: @0, @"version": job[@"version"] ?: @"",
                       @"stage": stage, @"error": error }];
    [_ctx postChange];
}

// 读取 rk-updater 留下的结果并上报。
- (void)consumeResult {
    NSString *path = [self.dir stringByAppendingPathComponent:@"result.plist"];
    NSDictionary *r = [NSDictionary dictionaryWithContentsOfFile:path];
    if (!r) return;
    [[NSFileManager defaultManager] removeItemAtPath:path error:nil];
    NSString *status = r[@"status"];
    if (_installTimer) dispatch_source_cancel(_installTimer);
    _installTimer = nil;
    _state = @"idle";
    _job = nil;
    NSMutableDictionary *p = [@{ @"release_id": r[@"release_id"] ?: @0, @"version": r[@"version"] ?: @"",
                                 @"from": r[@"from"] ?: @"" } mutableCopy];
    if (r[@"error"]) p[@"error"] = r[@"error"];
    if ([status isEqualToString:@"installed"]) {
        [_ctx emitEvent:@"system.update.installed" level:@"notice" payload:p];
        notify_post(RKUpdatedNote);
    } else if ([status isEqualToString:@"rolled_back"]) {
        [_ctx emitEvent:@"system.update.rolled_back" level:@"warning" payload:p];
    } else {
        p[@"stage"] = @"install";
        [_ctx emitEvent:@"system.update.failed" level:@"error" payload:p];
    }
    os_log(_ctx.log, "update result: %{public}@ %{public}@", status, r[@"version"]);
}

// App 收到「已更新」后请求重新打开：先让它退出，再由 daemon 拉起新版。
- (void)relaunchApp {
    __weak typeof(self) weakSelf = self;
    dispatch_after(dispatch_time(DISPATCH_TIME_NOW, (int64_t)(0.8 * NSEC_PER_SEC)), _ctx.queue, ^{
        if (!weakSelf) return;
        RKRun(@[ [RKJailbreakRoot() stringByAppendingString:@"/usr/bin/uiopen"], @"--bundleid", @"com.icloudza.residentkit" ]);
    });
}

#pragma mark - 加密工具

- (BOOL)verifySignature:(NSString *)sigB64 message:(NSData *)msg {
    NSData *pub = [[NSData alloc] initWithBase64EncodedString:RK_RELEASE_PUBLIC_KEY options:0];
    NSData *sig = [[NSData alloc] initWithBase64EncodedString:sigB64 options:0];
    if (pub.length != 65 || !sig.length) return NO;
    NSDictionary *attrs = @{ (id)kSecAttrKeyType: (id)kSecAttrKeyTypeECSECPrimeRandom,
                             (id)kSecAttrKeyClass: (id)kSecAttrKeyClassPublic,
                             (id)kSecAttrKeySizeInBits: @256 };
    SecKeyRef key = SecKeyCreateWithData((__bridge CFDataRef)pub, (__bridge CFDictionaryRef)attrs, NULL);
    if (!key) return NO;
    BOOL ok = SecKeyVerifySignature(key, kSecKeyAlgorithmECDSASignatureMessageX962SHA256,
                                    (__bridge CFDataRef)msg, (__bridge CFDataRef)sig, NULL);
    CFRelease(key);
    return ok;
}

@end
