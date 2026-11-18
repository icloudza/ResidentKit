// rkctl — residentd 的命令行 XPC 客户端，用于通过 SSH 测试。
//
//   rkctl ping | status | metrics | on | off
//   rkctl module <id> <command> [json-arguments]   例：module example.heartbeat set '{"on":true}'
//   rkctl mem <pid> | post <name>
//
// 用 tools/rkctl.entitlements（含客户端 entitlement）签名。
// 不带该 entitlement 签名，可用来验证 daemon 会拒绝外部客户端。

#import <Foundation/Foundation.h>
#import <notify.h>
#import "../common/RKProtocol.h"
#import "../common/RKXPCCompat.h"
#import "../common/RKMemorystatus.h"

// rkctl mem <pid> — 内核对 daemon 实际生效的值：真实内存占用（jetsam 的
// 计量口径）、生命周期峰值、jetsam band、内存上限。
static int print_mem(pid_t pid) {
    struct rusage_info_v4 ru = {0};
    if (proc_pid_rusage(pid, RUSAGE_INFO_V4, (rusage_info_t *)&ru) != 0) {
        perror("proc_pid_rusage"); return 1;
    }
    rk_memorystatus_priority_entry_t pe = {0};
    int pr = memorystatus_control(MEMORYSTATUS_CMD_GET_PRIORITY_LIST, pid, 0,
                                  &pe, sizeof(pe));
    rk_memorystatus_memlimit_t ml = {0};
    int lr = memorystatus_control(MEMORYSTATUS_CMD_GET_MEMLIMIT_PROPERTIES, pid, 0,
                                  &ml, sizeof(ml));
    printf("pid=%d footprint=%.2fMB peak=%.2fMB resident=%.2fMB "
           "jetsam_band=%s limit_active=%dMB limit_inactive=%dMB\n",
           pid, ru.ri_phys_footprint / 1048576.0,
           ru.ri_lifetime_max_phys_footprint / 1048576.0,
           ru.ri_resident_size / 1048576.0,
           pr > 0 ? [NSString stringWithFormat:@"%d", pe.priority].UTF8String : "?",
           lr == 0 ? ml.memlimit_active : -1, lr == 0 ? ml.memlimit_inactive : -1);
    return 0;
}

int main(int argc, char *argv[]) {
    @autoreleasepool {
        NSString *cmd = argc > 1 ? @(argv[1]) : @"status";
        if ([cmd isEqualToString:@"mem"]) {
            if (argc < 3) { fprintf(stderr, "usage: rkctl mem <pid>\n"); return 64; }
            return print_mem(atoi(argv[2]));
        }
        if ([cmd isEqualToString:@"post"]) {          // 发送 Darwin 通知
            if (argc < 3) { fprintf(stderr, "usage: rkctl post <name>\n"); return 64; }
            return notify_post(argv[2]) == NOTIFY_STATUS_OK ? 0 : 1;
        }

        NSXPCConnection *conn =
            RKConnectionToMachService(@(RK_MACH_SERVICE), NSXPCConnectionPrivileged);
        conn.remoteObjectInterface = RKDaemonInterface();
        [conn resume];

        __block int rc = 0;
        dispatch_semaphore_t done = dispatch_semaphore_create(0);
        id<RKDaemonProtocol> d = [conn remoteObjectProxyWithErrorHandler:^(NSError *e) {
            fprintf(stderr, "xpc error: %s\n", e.description.UTF8String);
            rc = 2;
            dispatch_semaphore_signal(done);
        }];

        if ([cmd isEqualToString:@"ping"]) {
            [d pingWithReply:^(NSString *v, uint64_t pid) {
                printf("version=%s pid=%llu\n", v.UTF8String, pid);
                dispatch_semaphore_signal(done);
            }];
        } else if ([cmd isEqualToString:@"on"] || [cmd isEqualToString:@"off"]) {
            [d setEnabled:[cmd isEqualToString:@"on"] reply:^(BOOL now) {
                printf("enabled=%d\n", now);
                dispatch_semaphore_signal(done);
            }];
        } else if ([cmd isEqualToString:@"metrics"]) {
            [d queryMetricsWithReply:^(NSDictionary *m) {
                for (NSString *k in [m.allKeys sortedArrayUsingSelector:@selector(compare:)]) {
                    printf("%-14s %s\n", k.UTF8String, [m[k] description].UTF8String);
                }
                dispatch_semaphore_signal(done);
            }];
        } else if ([cmd isEqualToString:@"status"]) {
            [d queryStatusWithReply:^(NSDictionary *s) {
                printf("enabled=%d pid=%d uptime=%.0f version=%s watchdog=%d keepalive=%d modules=%s\n",
                       [s[RKStatusEnabled] boolValue], [s[RKStatusPID] intValue],
                       [s[RKStatusUptime] doubleValue],
                       [s[RKStatusVersion] description].UTF8String,
                       [s[RKStatusWatchdogOK] boolValue], [s[RKStatusKeepAliveOK] boolValue],
                       [s[RKStatusModules] componentsJoinedByString:@","].UTF8String);
                dispatch_semaphore_signal(done);
            }];
        } else if ([cmd isEqualToString:@"module"] && argc >= 4) {
            NSDictionary *args = @{};
            if (argc >= 5) {
                NSData *json = [@(argv[4]) dataUsingEncoding:NSUTF8StringEncoding];
                args = [NSJSONSerialization JSONObjectWithData:json options:0 error:nil];
                if (![args isKindOfClass:NSDictionary.class]) {
                    fprintf(stderr, "arguments must be a JSON object\n"); return 64;
                }
            }
            [d invokeModule:@(argv[2]) command:@(argv[3]) arguments:args
                      reply:^(NSDictionary *r, NSError *e) {
                if (e) { fprintf(stderr, "error %ld: %s\n", (long)e.code, e.localizedDescription.UTF8String); rc = 1; }
                else printf("%s\n", r.description.UTF8String);
                dispatch_semaphore_signal(done);
            }];
        } else {
            fprintf(stderr, "usage: rkctl ping|status|metrics|on|off|module <id> <cmd> [json]|mem <pid>|post <name>\n");
            return 64;
        }

        if (dispatch_semaphore_wait(done,
                dispatch_time(DISPATCH_TIME_NOW, 5 * NSEC_PER_SEC))) {
            fprintf(stderr, "timeout\n");
            rc = 3;
        }
        [conn invalidate];
        return rc;
    }
}
