// RKNetLinkModule — net.link：到服务端的常驻双向 WebSocket 连接，基于
// Network.framework（不用 NSURLSession：更轻，且直接提供路径监测和 TCP keepalive）。
//
// 线上格式：每个文本帧一个 JSON 对象
//     {"id": "...", "type": "...", "payload": {...}, "ts": <unix 秒>}
//   设备 → 服务端   hello、ping、result、<"send" 发出的事件类型>
//   服务端 → 设备   ack、pong、ping、invoke
//
// 可靠性
//   * 网络路径监测：网络恢复或接口切换（Wi-Fi ↔ 蜂窝）时立即重连，无网时静默。
//   * 应用层心跳每 25 秒；70 秒无任何入站数据即认为连接已死（NAT 超时、TCP 半开）并重建。
//   * 重连指数退避 1 → 60 秒，另加 0–30% 抖动。
//   * 上行事件（"send"）进持久化队列，每次重连后重发直到服务端 ack 对应 id
//     （至少一次，服务端按 id 去重）。队列在 daemon 重新拉起后仍在。
//   * 下行 "invoke" 调用其他模块的命令，并以同 id 的 "result" 回复——新增远程命令只需新增模块。
//   * keep_awake：连接应保持期间持有 PreventUserIdleSystemSleep 电源断言，息屏时系统
//     （及 Wi-Fi）不休眠。daemon 退出时内核自动释放。
//
// 安全
//   * 证书固定：连默认服务器时用打包写入的指纹（公钥指纹，证书续期不变，见 RKTLS.h）；
//     手动填的其他地址首次连接自动信任（TOFU），之后证书必须一致。更换服务器地址时清空。
//   * 远程 "invoke" 只能调用模块在 +remoteCommands 中列出的命令（见 RKModule.h），其余拒绝。
//   * 设置（token、pin）存放在 daemon 仅 root 可访问的 state 目录。
//
// 设置：on、url（ws:// 或 wss://）、token（以 "Authorization: Bearer" 发送）、
// pin、keep_awake、device_id（只生成一次）。url / pin 未设置时用打包写入的默认服务器
// （RK_DEFAULT_URL / RK_DEFAULT_PIN），token 由 net.enroll 自动申请。
//
// 命令：status | configure {url, token} | set {on, keep_awake}
//       | send {type, payload} | reconnect | info

#import "RKModule.h"
#import "../../common/RKProtocol.h"
#import "RKMetrics.h"
#import "../RKTLS.h"
#import "../RKSystem.h"
#import <Network/Network.h>
#import <Security/Security.h>
#import <CommonCrypto/CommonDigest.h>
#import <unistd.h>

#pragma mark - IOKit 电源断言（iOS 无公开头文件）

typedef uint32_t RKIOPMAssertionID;
extern int IOPMAssertionCreateWithName(CFStringRef type, uint32_t level,
                                       CFStringRef name, RKIOPMAssertionID *outID);
extern int IOPMAssertionRelease(RKIOPMAssertionID assertionID);
static const uint32_t RKIOPMAssertionLevelOn = 255;

#pragma mark -

static NSString *const RKLinkID = @"net.link";
// 心跳间隔可远程配置（heartbeat_s，默认 25 秒）；超过 2.8 个心跳没有任何入站帧视为断开。
static const double RKConnectTimeout = 20;       // s，从 start 到 ready
static const double RKMaxBackoff = 60;           // s
static const NSUInteger RKQueueLimit = 500;      // 超出则丢弃最旧的
static const NSUInteger RKRecentLimit = 30;
static const uint16_t RKCloseTokenRevoked = 4401;   // 服务端关闭码：令牌已吊销（gateway.CloseTokenRevoked）

typedef NS_ENUM(NSInteger, RKLinkState) {
    RKLinkOff, RKLinkUnconfigured, RKLinkNoNetwork, RKLinkConnecting, RKLinkConnected, RKLinkRetrying,
};

static NSString *RKLinkStateName(RKLinkState s) {
    switch (s) {
        case RKLinkOff:          return @"off";
        case RKLinkUnconfigured: return @"unconfigured";
        case RKLinkNoNetwork:    return @"no_network";
        case RKLinkConnecting:   return @"connecting";
        case RKLinkConnected:    return @"connected";
        case RKLinkRetrying:     return @"retrying";
    }
    return @"?";
}

static double RKNow(void) { return [NSDate date].timeIntervalSince1970; }

// NSJSONSerialization 只接受 JSON 类型，而模块结果可能含日期或 data：
// 日期 → unix 秒，data → base64。
static id RKJSONSafe(id obj) {
    if ([obj isKindOfClass:NSDictionary.class]) {
        NSMutableDictionary *d = [NSMutableDictionary dictionary];
        [obj enumerateKeysAndObjectsUsingBlock:^(id k, id v, BOOL *stop) {
            d[[k description]] = RKJSONSafe(v);
        }];
        return d;
    }
    if ([obj isKindOfClass:NSArray.class]) {
        NSMutableArray *a = [NSMutableArray array];
        for (id v in obj) [a addObject:RKJSONSafe(v)];
        return a;
    }
    if ([obj isKindOfClass:NSDate.class]) return @([obj timeIntervalSince1970]);
    if ([obj isKindOfClass:NSData.class]) return [obj base64EncodedStringWithOptions:0];
    if ([obj isKindOfClass:NSNumber.class]) {
        double d = [obj doubleValue];   // JSON 没有 NaN / Infinity
        return (isnan(d) || isinf(d)) ? NSNull.null : obj;
    }
    if ([obj isKindOfClass:NSString.class] || obj == NSNull.null) return obj;
    return obj ? [obj description] : NSNull.null;
}

// 绝不抛异常：NSJSONSerialization 遇到非法输入会 raise，在 daemon 里就是可被远程触发的崩溃。
static NSData *RKJSONEncode(id obj) {
    id safe = RKJSONSafe(obj);
    if (![NSJSONSerialization isValidJSONObject:safe]) return nil;
    return [NSJSONSerialization dataWithJSONObject:safe options:0 error:nil];
}

@interface RKNetLinkModule : NSObject <RKModule>
@end

@implementation RKNetLinkModule {
    RKModuleContext *_ctx;
    RKLinkState _state;

    nw_connection_t _conn;
    uint64_t _generation;              // 每次连接自增，过期回调比对后直接返回
    nw_path_monitor_t _monitor;
    BOOL _pathOK;
    NSString *_network;                // wifi / cellular / wired / other / none

    dispatch_source_t _retryTimer, _heartbeatTimer, _connectTimer;
    NSUInteger _attempt;               // 连续失败次数，决定退避时长

    NSMutableArray<NSDictionary *> *_queue;   // 未 ack 的上行事件
    NSMutableArray<NSDictionary *> *_recent;  // 最近的帧，供 app 展示

    double _connectedAt, _lastRx, _rttMs, _pingSentAt;
    NSString *_pingID;
    uint64_t _sent, _received, _reconnects;
    NSString *_lastError;

    RKIOPMAssertionID _assertion;
    BOOL _assertionHeld;
    BOOL _pinFailed;                   // 由 TLS verify block 设置，失败时读取
    NSString *_seenPin;                // 本次连接看到的自签证书指纹，首次连接成功后保存
    BOOL _caTrusted;                   // 本次连接的证书由受信任 CA 签发（无需指纹）

    // 事件（docs/events.md）
    NSString *_reportedNetwork;        // 上次上报（或初始）的网络类型；nil 表示尚未取得
    dispatch_source_t _netTimer;       // net.network.changed 的 10 秒防抖
    double _downSince;                 // 已连接后断开的时刻；0 表示没有断开
    NSString *_downReason;
    uint64_t _downReconnectsBase;
}

RK_REGISTER_MODULE()

+ (NSString *)moduleIdentifier { return RKLinkID; }
+ (NSArray<NSString *> *)remoteCommands { return @[ @"status", @"info" ]; }

+ (NSDictionary<NSString *, NSDictionary *> *)commandSchema {
    return @{ @"status": @{
        @"label": @"连接状态", @"desc": @"设备与服务器连接的详细状态",
        @"result": @[
            @{ @"key": @"state", @"type": @"string", @"desc": @"connected 已连接 · connecting · retrying · no_network · off" },
            @{ @"key": @"network", @"type": @"string", @"desc": @"wifi · cellular · wired · other · none" },
            @{ @"key": @"rtt_ms", @"type": @"int", @"desc": @"心跳往返延迟（毫秒），已连接时才有" },
            @{ @"key": @"connected_since", @"type": @"number", @"desc": @"本次连接建立的时间（Unix 秒）" },
            @{ @"key": @"reconnects", @"type": @"int", @"desc": @"daemon 启动以来的重连次数" },
            @{ @"key": @"queued", @"type": @"int", @"desc": @"待发送（未确认）的事件数" },
            @{ @"key": @"keep_awake", @"type": @"bool", @"desc": @"是否开启锁屏保持连接" },
        ] },
        @"info": @{
        @"label": @"设备信息", @"desc": @"型号、系统、App 与服务版本，以及当前资源占用",
        @"timeout": @10,
        @"result": @[
            @{ @"key": @"info", @"type": @"object", @"desc": @"{model 机型标识, os 系统版本, app_version App 版本}" },
            @{ @"key": @"version", @"type": @"string", @"desc": @"服务（residentd）版本" },
            @{ @"key": @"device", @"type": @"string", @"desc": @"设备标识" },
            @{ @"key": @"network", @"type": @"string", @"desc": @"wifi · cellular · wired · other · none" },
            @{ @"key": @"metrics", @"type": @"object", @"desc": @"当前资源占用：服务内存、整机内存与 CPU 等" },
        ] } };
}

// 连接地址、令牌、证书固定都是本机设置，不开放远程配置：服务端不能把设备引向别处。
+ (NSArray<NSDictionary *> *)configSchema {
    return @[ @{ @"key": @"heartbeat_s", @"type": @"int", @"default": @25, @"min": @10, @"max": @120,
                 @"label": @"心跳间隔（秒）", @"desc": @"同时决定指标上报频率；超过 2.8 个心跳没有回应视为断开" } ];
}

- (void)configDidChange:(NSDictionary *)changed {
    if (changed[@"heartbeat_s"] && _heartbeatTimer) {
        uint64_t iv = (uint64_t)self.heartbeatInterval * NSEC_PER_SEC;
        dispatch_source_set_timer(_heartbeatTimer, dispatch_time(DISPATCH_TIME_NOW, (int64_t)iv), iv, 3 * NSEC_PER_SEC);
    }
}

- (instancetype)initWithContext:(RKModuleContext *)context {
    if ((self = [super init])) {
        _ctx = context;
        _network = @"none";
        _recent = [NSMutableArray array];
        _queue = [NSMutableArray array];
        NSData *saved = [NSData dataWithContentsOfFile:self.queuePath];
        id list = saved ? [NSJSONSerialization JSONObjectWithData:saved options:0 error:nil] : nil;
        if ([list isKindOfClass:NSArray.class])
            for (id m in list) if ([m isKindOfClass:NSDictionary.class]) [_queue addObject:m];
        if (![_ctx settingForKey:@"device_id"]) [_ctx setSetting:NSUUID.UUID.UUIDString forKey:@"device_id"];
    }
    return self;
}

#pragma mark - 设置
// 未设置过开关时：包里带了默认服务器就默认开启（自动接入），否则默认关闭。
- (BOOL)isOn {
    id on = [_ctx settingForKey:@"on"];
    return on ? [on boolValue] : strlen(RK_DEFAULT_URL) > 0;
}
- (long long)heartbeatInterval { return [[_ctx config:@"heartbeat_s"] longLongValue] ?: 25; }
- (BOOL)keepAwake    { return [[_ctx settingForKey:@"keep_awake"] boolValue]; }
- (NSString *)token  { return [_ctx settingForKey:@"token"]; }
// 地址：App 里设置的优先，否则用打包时写入的默认服务器。
- (NSString *)urlString {
    NSString *s = [_ctx settingForKey:@"url"];
    return s.length ? s : @(RK_DEFAULT_URL);
}
- (BOOL)usingDefaultServer { return ![[_ctx settingForKey:@"url"] length] && strlen(RK_DEFAULT_URL) > 0; }
- (NSURL *)url {
    NSString *s = self.urlString;
    NSURL *u = s.length ? [NSURL URLWithString:s] : nil;
    return ([u.scheme isEqualToString:@"ws"] || [u.scheme isEqualToString:@"wss"]) && u.host.length ? u : nil;
}
- (BOOL)configured   { return self.url != nil && self.token.length > 0; }
- (BOOL)shouldRun    { return _ctx.serviceEnabled && self.isOn && self.configured; }
// 信任的指纹：已保存的（首次信任或手动设置）加上连默认服务器时打包写入的（可多个，见 RKTLS.h）。
// 为空表示尚未信任任何证书，下次连接时首次信任。
- (NSArray<NSString *> *)pins {
    NSMutableArray *out = [RKNormalizePins([_ctx settingForKey:@"pin"]) mutableCopy];
    if (self.usingDefaultServer)
        for (NSString *p in RKNormalizePins(@(RK_DEFAULT_PIN))) if (![out containsObject:p]) [out addObject:p];
    return out;
}
- (NSString *)pin { return self.pins.firstObject; }   // 状态展示用
// 用 JSON 而非 plist：payload 可能含 null，plist 存不了。
- (NSString *)queuePath { return [_ctx.dataDirectory stringByAppendingPathComponent:@"queue.json"]; }

#pragma mark - 生命周期
- (void)serviceDidStart { [self apply]; }
- (void)serviceWillStop { [self apply]; }

// 唯一负责让“实际运行”与“应当运行”保持一致的地方。
- (void)apply {
    [self applyPowerAssertion];
    if (!self.shouldRun) {
        [self stopMonitor];
        [self closeConnection];
        [self cancelTimer:&_retryTimer];
        [self setState:(_ctx.serviceEnabled && self.isOn && !self.configured) ? RKLinkUnconfigured : RKLinkOff];
        return;
    }
    if (!_monitor) {
        [self setState:RKLinkConnecting];
        [self startMonitor];   // 首次回调会触发连接
    }
    else if (_state != RKLinkConnected && _state != RKLinkConnecting) [self connectNow];
}

- (void)setState:(RKLinkState)s {
    if (_state == s) return;
    os_log(_ctx.log, "%{public}@ -> %{public}@", RKLinkStateName(_state), RKLinkStateName(s));
    _state = s;
    [_ctx postChange];
}

- (void)applyPowerAssertion {
    BOOL want = self.shouldRun && self.keepAwake;
    if (want && !_assertionHeld) {
        int r = IOPMAssertionCreateWithName(CFSTR("PreventUserIdleSystemSleep"), RKIOPMAssertionLevelOn,
                                            CFSTR("com.icloudza.residentd net.link"), &_assertion);
        _assertionHeld = (r == 0);
        if (r) os_log_error(_ctx.log, "power assertion failed: 0x%x", r);
        else os_log(_ctx.log, "power assertion held");
    } else if (!want && _assertionHeld) {
        IOPMAssertionRelease(_assertion);
        _assertionHeld = NO;
        os_log(_ctx.log, "power assertion released");
    }
}

// 丢弃当前连接，让 -apply 从头重新决定。用于配置变更后：单独 closeConnection
// 不会改 _state，-apply 会误以为旧连接还在。
- (void)restart {
    _attempt = 0;
    [self closeConnection];
    [self cancelTimer:&_retryTimer];
    _state = RKLinkOff;
    [self apply];
}

#pragma mark - 网络路径
- (void)startMonitor {
    _monitor = nw_path_monitor_create();
    nw_path_monitor_set_queue(_monitor, _ctx.queue);
    __weak typeof(self) weakSelf = self;
    nw_path_monitor_set_update_handler(_monitor, ^(nw_path_t path) {
        [weakSelf pathChanged:path];
    });
    nw_path_monitor_start(_monitor);
}

- (void)stopMonitor {
    if (_monitor) nw_path_monitor_cancel(_monitor);
    _monitor = nil;
    _pathOK = NO;
    _network = @"none";
    _reportedNetwork = nil;   // 监听停止期间的变化不上报
    [self cancelTimer:&_netTimer];
}

// 网络类型变化后等 10 秒再上报，期间来回切换只算一次。
- (void)scheduleNetworkEvent {
    if (!_reportedNetwork) { _reportedNetwork = _network; return; }   // 首次取得，不上报
    [self cancelTimer:&_netTimer];
    __weak typeof(self) weakSelf = self;
    _netTimer = [self oneShotAfter:10 do:^{
        typeof(self) self_ = weakSelf;
        if (!self_) return;
        self_->_netTimer = nil;
        if ([self_->_network isEqualToString:self_->_reportedNetwork]) return;
        [self_->_ctx emitEvent:@"net.network.changed" level:@"info"
                       payload:@{ @"from": self_->_reportedNetwork, @"to": self_->_network }];
        self_->_reportedNetwork = self_->_network;
    }];
}

// 记录「已连接 → 断开」的时刻，恢复时据此发 net.link.recovered。
- (void)markDown:(NSString *)reason {
    if (_state != RKLinkConnected || _downSince > 0) return;
    _downSince = RKNow();
    _downReason = reason;
    _downReconnectsBase = _reconnects;
}

- (void)pathChanged:(nw_path_t)path {
    BOOL ok = nw_path_get_status(path) == nw_path_status_satisfied;
    NSString *net = !ok ? @"none"
        : nw_path_uses_interface_type(path, nw_interface_type_wifi) ? @"wifi"
        : nw_path_uses_interface_type(path, nw_interface_type_cellular) ? @"cellular"
        : nw_path_uses_interface_type(path, nw_interface_type_wired) ? @"wired" : @"other";
    BOOL changed = ![net isEqualToString:_network];
    _pathOK = ok;
    _network = net;
    if (!self.shouldRun) return;
    if (changed || !_reportedNetwork) [self scheduleNetworkEvent];
    if (!ok) {
        os_log(_ctx.log, "network lost");
        [self markDown:@"网络断开"];
        [self closeConnection];
        [self cancelTimer:&_retryTimer];
        [self setState:RKLinkNoNetwork];
        return;
    }
    // 网络恢复或换了接口：旧 socket 多半已死或绑在错误接口上，立即重连。
    if (changed || _state == RKLinkNoNetwork || _state == RKLinkOff || _state == RKLinkUnconfigured) {
        os_log(_ctx.log, "network %{public}@ — reconnecting", net);
        _attempt = 0;
        [self connectNow];
    }
}

#pragma mark - 连接
- (void)cancelTimer:(dispatch_source_t __strong *)t {
    if (*t) dispatch_source_cancel(*t);
    *t = nil;
}

- (dispatch_source_t)oneShotAfter:(double)seconds do:(dispatch_block_t)block {
    dispatch_source_t t = dispatch_source_create(DISPATCH_SOURCE_TYPE_TIMER, 0, 0, _ctx.queue);
    dispatch_source_set_timer(t, dispatch_time(DISPATCH_TIME_NOW, (int64_t)(seconds * NSEC_PER_SEC)),
                              DISPATCH_TIME_FOREVER, (uint64_t)(0.1 * seconds * NSEC_PER_SEC));
    dispatch_source_set_event_handler(t, block);
    dispatch_resume(t);
    return t;
}

- (void)closeConnection {
    _generation++;
    if (_conn) nw_connection_cancel(_conn);
    _conn = nil;
    [self cancelTimer:&_heartbeatTimer];
    [self cancelTimer:&_connectTimer];
    _pingID = nil;
}

- (void)connectNow {
    [self cancelTimer:&_retryTimer];
    [self closeConnection];
    _pinFailed = NO;
    _seenPin = nil;
    _caTrusted = NO;
    if (!self.shouldRun) return;
    if (!_pathOK) { [self setState:RKLinkNoNetwork]; return; }

    NSURL *url = self.url;
    BOOL tls = [url.scheme isEqualToString:@"wss"];
    nw_endpoint_t endpoint = nw_endpoint_create_url(url.absoluteString.UTF8String);
    NSArray<NSString *> *pins = self.pins;
    nw_parameters_configure_protocol_block_t tlsConfig = NW_PARAMETERS_DISABLE_PROTOCOL;
    if (tls) {
        __weak typeof(self) weakSelf = self;
        dispatch_queue_t queue = _ctx.queue;
        tlsConfig = ^(nw_protocol_options_t options) {
            sec_protocol_options_t sec = nw_tls_copy_sec_protocol_options(options);
            sec_protocol_options_set_min_tls_protocol_version(sec, tls_protocol_version_TLSv12);
            NSString *host = url.host;
            sec_protocol_options_set_verify_block(sec, ^(sec_protocol_metadata_t md, sec_trust_t trust,
                                                         sec_protocol_verify_complete_t complete) {
                // 见 RKTLS.h：CA 签发的域名证书直接通过（不保存指纹）；否则看指纹，
                // 尚无指纹时信任本次证书（TOFU），连接就绪后才保存。
                // 系统校验可能访问网络（吊销检查），放到后台队列，避免卡住模块队列。
                SecTrustRef ref = sec_trust_copy_ref(trust);
                dispatch_async(dispatch_get_global_queue(QOS_CLASS_UTILITY, 0), ^{
                    BOOL ca = RKTrustIsSystemValid(ref, host);
                    NSString *seen = RKLeafCertificatePin(ref);
                    BOOL ok = ca || (seen && (!pins.count || RKTrustMatchesPins(ref, pins)));
                    CFRelease(ref);
                    dispatch_async(queue, ^{
                        typeof(self) self_ = weakSelf;
                        if (self_) {
                            self_->_seenPin = ca ? nil : seen;
                            self_->_caTrusted = ca;
                            if (!ok) [self_ pinMismatch];
                        }
                        complete(ok);
                    });
                });
            }, queue);
        };
    }
    nw_parameters_t params = nw_parameters_create_secure_tcp(tlsConfig,
        ^(nw_protocol_options_t tcp) {
            // 应用层心跳之外再加一层内核级存活检测。
            nw_tcp_options_set_enable_keepalive(tcp, true);
            nw_tcp_options_set_keepalive_idle_time(tcp, 30);
            nw_tcp_options_set_keepalive_interval(tcp, 10);
            nw_tcp_options_set_keepalive_count(tcp, 3);
            nw_tcp_options_set_connection_timeout(tcp, 15);
        });
    nw_protocol_options_t ws = nw_ws_create_options(nw_ws_version_13);
    nw_ws_options_add_additional_header(ws, "Authorization",
        [@"Bearer " stringByAppendingString:self.token].UTF8String);
    nw_ws_options_set_auto_reply_ping(ws, true);
    nw_ws_options_set_maximum_message_size(ws, 1 << 20);
    nw_protocol_stack_prepend_application_protocol(nw_parameters_copy_default_protocol_stack(params), ws);

    nw_connection_t conn = nw_connection_create(endpoint, params);
    nw_connection_set_queue(conn, _ctx.queue);
    uint64_t gen = _generation;
    __weak typeof(self) weakSelf = self;
    nw_connection_set_state_changed_handler(conn, ^(nw_connection_state_t st, nw_error_t err) {
        typeof(self) self_ = weakSelf;
        if (!self_ || gen != self_->_generation) return;
        switch (st) {
            case nw_connection_state_ready:   [self_ connectionReady]; break;
            // waiting = 当前被拒/不可达；改用自己的退避策略。
            case nw_connection_state_waiting:
            case nw_connection_state_failed:  [self_ failWith:err fallback:@"连接失败"]; break;
            default: break;
        }
    });
    _conn = conn;
    [self setState:RKLinkConnecting];
    _connectTimer = [self oneShotAfter:RKConnectTimeout do:^{
        typeof(self) self_ = weakSelf;
        if (self_ && gen == self_->_generation && self_->_state == RKLinkConnecting)
            [self_ failWith:nil fallback:@"连接超时"];
    }];
    os_log(_ctx.log, "connecting to %{public}@ (attempt %lu)", url.host, (unsigned long)_attempt + 1);
    nw_connection_start(conn);
}

// 服务端告知令牌已被吊销：不再用它重试（否则反复鉴权失败），丢弃令牌并立即转入自动接入，
// 由管理员在控制台重新批准。
- (void)tokenRevoked {
    os_log_error(_ctx.log, "token revoked by server — dropping it and re-enrolling");
    [_ctx setSetting:nil forKey:@"token"];
    [self markDown:@"令牌已被吊销"];
    _lastError = @"令牌已被吊销，正在重新申请接入";
    [self restart];
    [_ctx invokeModule:@"net.enroll" command:@"retry" arguments:nil reply:^(NSDictionary *r, NSError *e) {}];
}

// 在模块队列上执行（verify block 被调度到该队列）。
- (void)pinMismatch {
    _pinFailed = YES;
    os_log_error(_ctx.log, "server certificate is neither CA-trusted nor pinned — refusing");
}

- (void)failWith:(nw_error_t)err fallback:(NSString *)fallback {
    NSString *reason = fallback;
    if (_pinFailed) {
        reason = @"服务器证书不受信任：不是受信任 CA 签发，也与已信任的指纹不一致（已拒绝）";
    } else if (err) {
        CFErrorRef cf = nw_error_copy_cf_error(err);
        if (cf) { reason = [(__bridge NSError *)cf localizedDescription]; CFRelease(cf); }
    }
    [self markDown:reason];
    _lastError = reason;
    os_log_error(_ctx.log, "link down: %{public}@", reason);
    [self closeConnection];
    [self scheduleRetry];
}

- (void)scheduleRetry {
    if (!self.shouldRun) return;
    if (!_pathOK) { [self setState:RKLinkNoNetwork]; return; }   // 由路径监测唤醒
    // 前 5 次快速重连（每次间隔 ≤3s，尽量不影响使用）；之后再指数退避。
    double base = _attempt < 5 ? 2.0 : MIN(RKMaxBackoff, pow(2, MIN(_attempt - 4, 6)));
    double delay = base * (1.0 + arc4random_uniform(300) / 1000.0);
    _attempt++;
    _reconnects++;
    [self setState:RKLinkRetrying];
    __weak typeof(self) weakSelf = self;
    _retryTimer = [self oneShotAfter:delay do:^{ [weakSelf connectNow]; }];
    os_log(_ctx.log, "retry in %.1fs", delay);
}

- (void)connectionReady {
    [self cancelTimer:&_connectTimer];
    _pinFailed = NO;
    if (!self.pins.count && _seenPin && [self.url.scheme isEqualToString:@"wss"]) {
        [_ctx setSetting:_seenPin forKey:@"pin"];   // TOFU：记住首次信任的证书
        os_log(_ctx.log, "trusted server certificate %{public}@", [_seenPin substringToIndex:16]);
    }
    _attempt = 0;
    _lastError = nil;
    _connectedAt = _lastRx = RKNow();
    [self setState:RKLinkConnected];

    char host[256] = {0};
    gethostname(host, sizeof(host) - 1);
    [self sendMessage:@{ @"id": NSUUID.UUID.UUIDString, @"type": @"hello", @"ts": @(RKNow()),
                         @"payload": @{ @"device": [_ctx settingForKey:@"device_id"] ?: @"",
                                        @"name": @(host), @"version": @(RK_VERSION),
                                        @"network": _network, @"info": RKDeviceInfo(),
                                        @"remote_commands": _ctx.remoteCommandMap,
                                        @"config_version": @(_ctx.configVersion),
                                        @"config_schema": _ctx.configSchemaMap,
                                        @"command_schema": _ctx.commandSchemaMap } }];
    // 按顺序重发所有未 ack 的消息，服务端按 id 去重。
    for (NSDictionary *m in [_queue copy]) [self sendMessage:m];

    // 断开 ≥ 30 秒后恢复才上报，短暂波动不计。
    if (_downSince > 0) {
        double down = RKNow() - _downSince;
        if (down >= 30) {
            [_ctx emitEvent:@"net.link.recovered" level:@"notice"
                    payload:@{ @"down_s": @((long)down), @"reason": _downReason ?: @"",
                               @"reconnects": @(_reconnects - _downReconnectsBase) }];
        }
        _downSince = 0;
        _downReason = nil;
    }

    __weak typeof(self) weakSelf = self;
    uint64_t gen = _generation;
    _heartbeatTimer = dispatch_source_create(DISPATCH_SOURCE_TYPE_TIMER, 0, 0, _ctx.queue);
    dispatch_source_set_timer(_heartbeatTimer,
        dispatch_time(DISPATCH_TIME_NOW, (int64_t)self.heartbeatInterval * NSEC_PER_SEC),
        (uint64_t)self.heartbeatInterval * NSEC_PER_SEC, 3 * NSEC_PER_SEC);
    dispatch_source_set_event_handler(_heartbeatTimer, ^{
        typeof(self) self_ = weakSelf;
        if (self_ && gen == self_->_generation) [self_ heartbeat];
    });
    dispatch_resume(_heartbeatTimer);

    [self receiveOn:_conn generation:gen];
}

- (void)heartbeat {
    if (RKNow() - _lastRx > self.heartbeatInterval * 2.8) {
        [self failWith:nil fallback:@"心跳超时"];
        return;
    }
    _pingID = NSUUID.UUID.UUIDString;
    _pingSentAt = RKNow();
    // 心跳顺带上报资源指标（内存 / CPU），供控制台展示；与 App 共用同一采样器。
    [self sendMessage:@{ @"id": _pingID, @"type": @"ping", @"ts": @(_pingSentAt),
                         @"payload": @{ @"metrics": RKSampleMetrics() } }];
}

#pragma mark - 收发帧
- (void)receiveOn:(nw_connection_t)conn generation:(uint64_t)gen {
    __weak typeof(self) weakSelf = self;
    nw_connection_receive_message(conn, ^(dispatch_data_t content, nw_content_context_t context,
                                          bool complete, nw_error_t error) {
        typeof(self) self_ = weakSelf;
        if (!self_ || gen != self_->_generation) return;
        if (error) { [self_ failWith:error fallback:@"接收失败"]; return; }
        nw_protocol_definition_t def = nw_protocol_copy_ws_definition();
        nw_protocol_metadata_t md = context ? nw_content_context_copy_protocol_metadata(context, def) : nil;
        nw_ws_opcode_t op = md ? nw_ws_metadata_get_opcode(md) : nw_ws_opcode_invalid;
        if (op == nw_ws_opcode_close || (!content && !md)) {
            if (md && nw_ws_metadata_get_close_code(md) == RKCloseTokenRevoked) {
                [self_ tokenRevoked];
                return;
            }
            [self_ failWith:nil fallback:@"服务器关闭了连接"];
            return;
        }
        self_->_lastRx = RKNow();
        if (content && op == nw_ws_opcode_text) {
            NSData *data = (NSData *)content;   // dispatch_data_t 可桥接为 NSData
            id msg = [NSJSONSerialization JSONObjectWithData:data options:0 error:nil];
            if ([msg isKindOfClass:NSDictionary.class]) [self_ handleMessage:msg];
        }
        [self_ receiveOn:conn generation:gen];
    });
}

- (void)sendMessage:(NSDictionary *)msg {
    if (!_conn || _state != RKLinkConnected) return;
    NSData *data = RKJSONEncode(msg);
    if (!data) { os_log_error(_ctx.log, "dropped un-encodable %{public}@", msg[@"type"]); return; }
    nw_protocol_metadata_t md = nw_ws_create_metadata(nw_ws_opcode_text);
    nw_content_context_t cc = nw_content_context_create("rk.msg");
    nw_content_context_set_metadata_for_protocol(cc, md);
    dispatch_data_t body = dispatch_data_create(data.bytes, data.length, _ctx.queue,
                                                DISPATCH_DATA_DESTRUCTOR_DEFAULT);
    nw_connection_send(_conn, body, cc, true, ^(nw_error_t e) {
        // 失败会经状态回调 / 心跳暴露，这里无需处理。
    });
    _sent++;
    [self note:@"out" message:msg];
}

- (void)note:(NSString *)dir message:(NSDictionary *)msg {
    NSString *type = msg[@"type"];
    if ([type isEqualToString:@"ping"] || [type isEqualToString:@"pong"]) return;   // 太频繁，不记录
    [_recent addObject:@{ @"t": @(RKNow()), @"dir": dir, @"type": type ?: @"?" }];
    while (_recent.count > RKRecentLimit) [_recent removeObjectAtIndex:0];
    [_ctx postChange];
}

- (void)handleMessage:(NSDictionary *)msg {
    _received++;
    NSString *type = msg[@"type"], *mid = msg[@"id"];
    [self note:@"in" message:msg];

    if ([type isEqualToString:@"pong"]) {
        if (mid && [mid isEqual:_pingID]) { _rttMs = (RKNow() - _pingSentAt) * 1000; _pingID = nil; }
    } else if ([type isEqualToString:@"ping"]) {
        [self sendMessage:@{ @"id": mid ?: @"", @"type": @"pong", @"ts": @(RKNow()) }];
    } else if ([type isEqualToString:@"ack"]) {
        NSUInteger i = [_queue indexOfObjectPassingTest:^BOOL(NSDictionary *m, NSUInteger idx, BOOL *stop) {
            return [m[@"id"] isEqual:mid];
        }];
        if (i != NSNotFound) { [_queue removeObjectAtIndex:i]; [self saveQueue]; }
    } else if ([type isEqualToString:@"invoke"]) {
        [self handleInvoke:msg];
    } else {
        os_log(_ctx.log, "ignored message type %{public}@", type);
    }
}

// 远程调用其他模块——仅限模块通过 +remoteCommands 开放的命令。net.link 只开放
// "status"，服务端无法改配置或切断自己的连接。
- (void)handleInvoke:(NSDictionary *)msg {
    NSString *mid = msg[@"id"] ?: @"";
    NSDictionary *p = [msg[@"payload"] isKindOfClass:NSDictionary.class] ? msg[@"payload"] : @{};
    NSString *module = p[@"module"], *command = p[@"command"];
    NSDictionary *args = [p[@"arguments"] isKindOfClass:NSDictionary.class] ? p[@"arguments"] : @{};
    __weak typeof(self) weakSelf = self;
    void (^answer)(NSDictionary *, NSError *) = ^(NSDictionary *r, NSError *e) {
        NSMutableDictionary *out = [@{ @"id": mid, @"type": @"result", @"ts": @(RKNow()) } mutableCopy];
        if (e) out[@"error"] = @{ @"code": @(e.code), @"message": e.localizedDescription ?: @"" };
        else out[@"payload"] = r ?: @{};
        if (!RKJSONEncode(out)) {
            [out removeObjectForKey:@"payload"];
            out[@"error"] = @{ @"code": @(RKErrorModuleFailed), @"message": @"结果无法编码为 JSON" };
        }
        [weakSelf sendMessage:out];
    };
    if (![module isKindOfClass:NSString.class] || ![command isKindOfClass:NSString.class]) {
        answer(nil, [_ctx errorWithCode:RKErrorInvalidArguments description:@"invoke 需要 module 和 command"]);
        return;
    }
    if (![_ctx module:module allowsRemoteCommand:command]) {
        os_log_error(_ctx.log, "refused remote %{public}@ %{public}@", module, command);
        answer(nil, [_ctx errorWithCode:RKErrorNotPermitted
                            description:[NSString stringWithFormat:@"%@ 未开放远程命令 %@", module, command]]);
        return;
    }
    [_ctx invokeModule:module command:command arguments:args reply:answer];
}

#pragma mark - 上行队列
- (void)saveQueue {
    if (_queue.count == 0) {
        [[NSFileManager defaultManager] removeItemAtPath:self.queuePath error:nil];
        return;
    }
    NSData *data = RKJSONEncode(_queue);
    if (!data || ![data writeToFile:self.queuePath options:NSDataWritingAtomic error:nil])
        os_log_error(_ctx.log, "could not persist the uplink queue");
}

// 事件规范校验（docs/events.md）：类型名、级别、payload ≤ 4 KB。返回错误说明，合规返回 nil。
static NSString *RKEventProblem(NSString *type, NSString *level, NSDictionary *payload) {
    static NSRegularExpression *re;
    static dispatch_once_t once;
    dispatch_once(&once, ^{
        re = [NSRegularExpression regularExpressionWithPattern:@"^[a-z0-9_]+(\\.[a-z0-9_]+)+$" options:0 error:nil];
    });
    if (type.length > 64 || ![re firstMatchInString:type options:0 range:NSMakeRange(0, type.length)])
        return @"type 需为「领域.对象.动作」格式：小写字母、数字、下划线，点分段，≤ 64 字符";
    if (![@[ @"info", @"notice", @"warning", @"error" ] containsObject:level])
        return @"level 只能是 info / notice / warning / error";
    NSData *data = RKJSONEncode(payload ?: @{});
    if (!data) return @"payload 无法编码为 JSON";
    if (data.length > 4096) return @"payload 超过 4 KB";
    return nil;
}

- (NSString *)enqueueType:(NSString *)type level:(NSString *)level payload:(NSDictionary *)payload {
    NSString *mid = NSUUID.UUID.UUIDString;
    // 以 JSON 安全形式存储，保证落盘和之后的发送都能成功。
    NSDictionary *m = @{ @"id": mid, @"type": type, @"level": level, @"ts": @(RKNow()),
                         @"payload": RKJSONSafe(payload ?: @{}) };
    if (!RKJSONEncode(m)) return nil;
    [_queue addObject:m];
    while (_queue.count > RKQueueLimit) [_queue removeObjectAtIndex:0];
    [self saveQueue];
    [self sendMessage:m];
    return mid;
}

#pragma mark - RKModule

- (NSDictionary *)moduleStatus {
    NSMutableDictionary *s = [@{
        @"state": RKLinkStateName(_state),
        @"on": @(self.isOn),
        @"keep_awake": @(self.keepAwake),
        @"awake_held": @(_assertionHeld),
        @"url": self.urlString,
        @"default_server": @(self.usingDefaultServer),
        @"has_token": @(self.token.length > 0),
        @"pin": self.pin ?: @"",
        @"ca_trusted": @(_caTrusted),
        @"device_id": [_ctx settingForKey:@"device_id"] ?: @"",
        @"network": _network,
        @"queued": @(_queue.count),
        @"sent": @(_sent),
        @"received": @(_received),
        @"reconnects": @(_reconnects),
        @"attempt": @(_attempt),
        @"recent": [_recent copy],
    } mutableCopy];
    if (_state == RKLinkConnected) {
        s[@"connected_since"] = @(_connectedAt);
        s[@"last_rx"] = @(_lastRx);
        if (_rttMs > 0) s[@"rtt_ms"] = @(round(_rttMs));
    }
    if (_lastError) s[@"last_error"] = _lastError;
    // 还没有令牌时附上自动接入的进度（含配对码），App 据此显示「等待控制台批准」。
    if (!self.token.length) {
        [_ctx invokeModule:@"net.enroll" command:@"status" arguments:nil reply:^(NSDictionary *r, NSError *e) {
            if (r) s[@"enroll"] = r;
        }];
    }
    return s;
}

- (void)handleCommand:(NSString *)command arguments:(NSDictionary *)args reply:(RKModuleReply)reply {
    if ([command isEqualToString:@"configure"]) {
        id url = args[@"url"], token = args[@"token"];
        if (url) {
            NSURL *u = [url isKindOfClass:NSString.class] ? [NSURL URLWithString:url] : nil;
            BOOL ok = [url isEqual:@""] || (([u.scheme isEqualToString:@"ws"] || [u.scheme isEqualToString:@"wss"]) && u.host.length);
            if (!ok) { reply(nil, [_ctx errorWithCode:RKErrorInvalidArguments description:@"地址需以 ws:// 或 wss:// 开头"]); return; }
            // 填的就是打包写入的默认服务器：按「使用默认」处理，保留打包写入的指纹。
            if ([url isEqual:@(RK_DEFAULT_URL)]) url = @"";
            // 换服务器：之前信任的证书作废，连上新服务器时重新信任。
            if (![url isEqual:[_ctx settingForKey:@"url"] ?: @""]) [_ctx setSetting:nil forKey:@"pin"];
            [_ctx setSetting:([url length] ? url : nil) forKey:@"url"];
        }
        if ([token isKindOfClass:NSString.class]) [_ctx setSetting:([token length] ? token : nil) forKey:@"token"];
        id pin = args[@"pin"];
        if ([pin isKindOfClass:NSString.class]) {
            NSString *norm = RKNormalizePin(pin);
            if ([pin length] && !norm) {
                reply(nil, [_ctx errorWithCode:RKErrorInvalidArguments description:@"证书指纹应为 64 位十六进制 SHA-256"]);
                return;
            }
            [_ctx setSetting:norm forKey:@"pin"];
        }
        // 接入码（扫控制台的接入二维码得到）：丢弃旧令牌，立即带着它重新申请接入，服务端核对后直接批准。
        id ticket = args[@"ticket"];
        if ([ticket isKindOfClass:NSString.class]) {
            [_ctx setSetting:([ticket length] ? ticket : nil) forKey:@"enroll_ticket"];
            if ([ticket length]) [_ctx setSetting:nil forKey:@"token"];
        }
        [self restart];
        if ([ticket isKindOfClass:NSString.class] && [ticket length])
            [_ctx invokeModule:@"net.enroll" command:@"retry" arguments:nil reply:^(NSDictionary *r, NSError *e) {}];
    } else if ([command isEqualToString:@"set"]) {
        if ([args[@"on"] isKindOfClass:NSNumber.class]) [_ctx setSetting:args[@"on"] forKey:@"on"];
        if ([args[@"keep_awake"] isKindOfClass:NSNumber.class]) [_ctx setSetting:args[@"keep_awake"] forKey:@"keep_awake"];
        [self apply];
    } else if ([command isEqualToString:@"send"]) {
        NSString *type = [args[@"type"] isKindOfClass:NSString.class] ? args[@"type"] : @"";
        NSString *level = [args[@"level"] isKindOfClass:NSString.class] ? args[@"level"] : @"info";
        NSDictionary *payload = [args[@"payload"] isKindOfClass:NSDictionary.class] ? args[@"payload"] : @{};
        NSString *problem = RKEventProblem(type, level, payload);
        if (problem) { reply(nil, [_ctx errorWithCode:RKErrorInvalidArguments description:problem]); return; }
        NSString *mid = [self enqueueType:type level:level payload:payload];
        if (!mid) { reply(nil, [_ctx errorWithCode:RKErrorInvalidArguments description:@"payload 无法编码为 JSON"]); return; }
        reply(@{ @"id": mid, @"queued": @(_queue.count) }, nil);
        return;
    } else if ([command isEqualToString:@"reconnect"]) {
        [self restart];
    } else if ([command isEqualToString:@"enroll_info"]) {
        // 仅本机：net.enroll 申请接入所需的地址、证书指纹与设备 ID。
        NSURL *u = self.url;
        if (!u || !self.isOn) {
            reply(nil, [_ctx errorWithCode:RKErrorModuleFailed description:u ? @"服务器连接已关闭" : @"服务器连接未配置"]);
            return;
        }
        NSURLComponents *c = [NSURLComponents componentsWithURL:u resolvingAgainstBaseURL:NO];
        c.scheme = [u.scheme isEqualToString:@"wss"] ? @"https" : @"http";
        c.path = @"/enroll";
        c.query = nil;
        reply(@{ @"url": c.URL.absoluteString ?: @"", @"pins": self.pins, @"has_token": @(self.token.length > 0),
                 @"device_id": [_ctx settingForKey:@"device_id"] ?: @"",
                 @"ticket": [_ctx settingForKey:@"enroll_ticket"] ?: @"" }, nil);
        return;
    } else if ([command isEqualToString:@"download_info"]) {
        // 仅本机（其他模块 / App）可调，不在 remoteCommands 里：返回下载 OTA 包所需的地址、令牌与证书指纹。
        NSURL *u = self.url;
        if (!u || !self.token.length) {
            reply(nil, [_ctx errorWithCode:RKErrorModuleFailed description:@"服务器连接未配置"]);
            return;
        }
        NSURLComponents *c = [NSURLComponents componentsWithURL:u resolvingAgainstBaseURL:NO];
        c.scheme = [u.scheme isEqualToString:@"wss"] ? @"https" : @"http";
        c.path = @"/ota/";
        c.query = nil;
        reply(@{ @"base_url": c.URL.absoluteString ?: @"", @"token": self.token, @"pins": self.pins }, nil);
        return;
    } else if ([command isEqualToString:@"info"]) {
        reply(@{ @"info": RKDeviceInfo(), @"version": @(RK_VERSION),
                 @"device": [_ctx settingForKey:@"device_id"] ?: @"", @"network": _network ?: @"none",
                 @"metrics": RKSampleMetrics() }, nil);
        return;
    } else {
        reply(nil, [_ctx errorWithCode:RKErrorUnknownCommand description:[@"未知命令 " stringByAppendingString:command]]);
        return;
    }
    [_ctx postChange];
    reply(self.moduleStatus, nil);
}

@end
