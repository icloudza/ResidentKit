// RKEnrollLink.m — 说明见头文件。

#import "RKEnrollLink.h"
#import "RKClient.h"

NSString *const RKURLScheme = @"residentkit";

@implementation RKEnrollLink

static BOOL RKValidServerURL(NSString *s) {
    NSURL *u = s.length ? [NSURL URLWithString:s] : nil;
    return ([u.scheme isEqualToString:@"wss"] || [u.scheme isEqualToString:@"ws"]) && u.host.length > 0;
}

static BOOL RKMatches(NSString *s, NSString *pattern) {
    if (!s.length) return NO;
    NSRegularExpression *re = [NSRegularExpression regularExpressionWithPattern:pattern options:0 error:nil];
    return [re numberOfMatchesInString:s options:0 range:NSMakeRange(0, s.length)] == 1;
}

+ (instancetype)linkWithString:(NSString *)string {
    NSString *s = [string stringByTrimmingCharactersInSet:NSCharacterSet.whitespaceAndNewlineCharacterSet];
    if (RKValidServerURL(s)) return [[self alloc] initWithURL:s ticket:nil pin:nil];

    NSURLComponents *c = [NSURLComponents componentsWithString:s];
    if (![c.scheme.lowercaseString isEqualToString:RKURLScheme] || ![c.host isEqualToString:@"enroll"]) return nil;
    NSMutableDictionary<NSString *, NSString *> *q = [NSMutableDictionary dictionary];
    for (NSURLQueryItem *it in c.queryItems) if (it.value) q[it.name] = it.value;
    if (!RKValidServerURL(q[@"u"])) return nil;
    NSString *t = q[@"t"], *p = q[@"p"];
    if (t.length && !RKMatches(t, @"^[A-Za-z0-9_-]{8,128}$")) return nil;
    if (p.length && !RKMatches(p.lowercaseString, @"^[0-9a-f]{64}(,[0-9a-f]{64})*$")) return nil;
    return [[self alloc] initWithURL:q[@"u"] ticket:t.length ? t : nil pin:p.length ? p.lowercaseString : nil];
}

- (instancetype)initWithURL:(NSString *)url ticket:(NSString *)ticket pin:(NSString *)pin {
    if ((self = [super init])) {
        _serverURL = url;
        _host = [NSURL URLWithString:url].host ?: url;
        _ticket = ticket;
        _pin = pin;
    }
    return self;
}

#pragma mark - 确认并应用

static void RKAlert(UIViewController *vc, NSString *title, NSString *msg) {
    UIAlertController *a = [UIAlertController alertControllerWithTitle:title message:msg
                                                        preferredStyle:UIAlertControllerStyleAlert];
    [a addAction:[UIAlertAction actionWithTitle:@"好" style:UIAlertActionStyleDefault handler:nil]];
    [vc presentViewController:a animated:YES completion:nil];
}

- (void)confirmAndApplyFrom:(UIViewController *)presenter {
    RKClient *client = RKClient.shared;
    __weak UIViewController *weakVC = presenter;
    // 先看当前连的是哪台：同一台且已接入、二维码又没带接入码时，无需重复操作。
    [client invokeModule:@"net.link" command:@"status" arguments:nil completion:^(NSDictionary *s, NSError *error) {
        UIViewController *vc = weakVC;
        if (!vc) return;
        if (!s) {
            RKAlert(vc, @"无法连接后台服务", error.localizedDescription ?: @"请确认服务已启用");
            return;
        }
        BOOL same = [s[@"url"] isEqual:self.serverURL];
        if (same && [s[@"has_token"] boolValue] && !self.ticket) {
            RKAlert(vc, @"已经连接这台服务器", self.host);
            return;
        }
        NSString *msg = same ? @"将用这个二维码重新申请接入。"
            : ([s[@"url"] length] ? [NSString stringWithFormat:@"当前连接的是 %@，切换后需要重新接入。",
                                     [NSURL URLWithString:s[@"url"]].host ?: s[@"url"]]
                                  : @"保存后立即申请接入。");
        if (!self.ticket) msg = [msg stringByAppendingString:@"\n申请后需要在控制台点「同意」。"];
        UIAlertController *a = [UIAlertController alertControllerWithTitle:
            [NSString stringWithFormat:@"连接到 %@？", self.host] message:msg preferredStyle:UIAlertControllerStyleAlert];
        [a addAction:[UIAlertAction actionWithTitle:@"取消" style:UIAlertActionStyleCancel handler:nil]];
        [a addAction:[UIAlertAction actionWithTitle:@"连接" style:UIAlertActionStyleDefault handler:^(UIAlertAction *x) {
            [self applyFrom:vc];
        }]];
        [vc presentViewController:a animated:YES completion:nil];
    }];
}

- (void)applyFrom:(UIViewController *)presenter {
    NSMutableDictionary *args = [@{ @"url": self.serverURL } mutableCopy];
    if (self.pin) args[@"pin"] = self.pin;
    if (self.ticket) args[@"ticket"] = self.ticket;
    __weak UIViewController *weakVC = presenter;
    [RKClient.shared invokeModule:@"net.link" command:@"configure" arguments:args
                       completion:^(NSDictionary *result, NSError *error) {
        UIViewController *vc = weakVC;
        if (!vc) return;
        if (error) { RKAlert(vc, @"保存失败", error.localizedDescription); return; }
        RKAlert(vc, @"已保存", self.ticket ? [NSString stringWithFormat:@"正在连接 %@，接入后自动上线。", self.host]
                                           : [NSString stringWithFormat:@"正在向 %@ 申请接入，请在控制台点「同意」。", self.host]);
    }];
}

@end
