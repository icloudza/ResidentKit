// RKSystem.m — 说明见头文件。

#import "RKSystem.h"
#import "../common/RKProtocol.h"
#import "../common/RKMemorystatus.h"
#import <os/log.h>
#import <fcntl.h>
#import <sys/sysctl.h>
#import <unistd.h>

// 解析成真实路径（/private/preboot/…/procursus），不经过 /var/jb 链接：
// Dopamine「隐藏越狱」会删掉这个链接，之后所有经它访问的路径都会失效。启动时解析一次并缓存。
NSString *RKJailbreakRoot(void) {
    static NSString *root;
    static dispatch_once_t once;
    dispatch_once(&once, ^{
        const char *jb = getenv("JBROOT");
        NSString *p = jb ? @(jb) : @"/var/jb";
        char real[PATH_MAX];
        root = p.length && realpath(p.fileSystemRepresentation, real) ? @(real) : p;
    });
    return root;
}

BOOL RKJailbreakHidden(void) {
    return RKJailbreakRoot().length > 0 && access("/var/jb", F_OK) != 0;
}

static NSString *RKSysctlString(const char *name) {
    size_t len = 0;
    if (sysctlbyname(name, NULL, &len, NULL, 0) != 0 || len == 0 || len > 256) return @"";
    char buf[257] = {0};
    if (sysctlbyname(name, buf, &len, NULL, 0) != 0) return @"";
    return @(buf) ?: @"";
}

NSDictionary<NSString *, NSString *> *RKDeviceInfo(void) {
    NSString *os = RKSysctlString("kern.osproductversion");
    NSString *plist = [RKJailbreakRoot() stringByAppendingString:@"/Applications/ResidentKit.app/Info.plist"];
    NSString *app = [NSDictionary dictionaryWithContentsOfFile:plist][@"CFBundleShortVersionString"];
    return @{ @"model": RKSysctlString("hw.machine"),
              @"os": os.length ? [@"iOS " stringByAppendingString:os] : @"",
              @"app_version": [app isKindOfClass:NSString.class] ? app : @"" };
}

NSString *RKStatePath(void) {
    return [RKJailbreakRoot() stringByAppendingString:@"/var/lib/residentd/state.plist"];
}

// launchd 忽略第三方 plist 的 JetsamProperties（iOS 18 / Dopamine 实测：拿到 band 40
// = BACKGROUND，低于前台 app 的 100，内存紧张时先被杀）。所以自己挪到 Apple daemon 的档位。
//
// 内存上限保持 launchd 默认（18 MB，fatal）：实际只用 ~2 MB，触顶即泄漏，被杀后重新拉起正合适。
void RKRaiseJetsamBand(void) {
    rk_memorystatus_priority_properties_t props = {
        .priority = RK_JETSAM_BAND_SYSTEM, .user_data = 0,
    };
    if (memorystatus_control(MEMORYSTATUS_CMD_SET_PRIORITY_PROPERTIES, getpid(),
                             0, &props, sizeof(props)) != 0) {
        os_log_error(OS_LOG_DEFAULT, "residentd: could not raise jetsam band: errno=%d", errno);
    }
}

uint64_t RKFootprintBytes(void) {
    struct rusage_info_v4 ru = {0};
    if (proc_pid_rusage(getpid(), RUSAGE_INFO_V4, (rusage_info_t *)&ru) != 0) return 0;
    return ru.ri_phys_footprint;
}

BOOL RKKeepAliveActive(void) {
    static BOOL active;
    static dispatch_once_t once;
    dispatch_once(&once, ^{
        NSString *plist = [NSString stringWithFormat:@"%@/Library/LaunchDaemons/%s.plist",
                           RKJailbreakRoot(), RK_LAUNCHD_LABEL];
        id keepAlive = [NSDictionary dictionaryWithContentsOfFile:plist][@"KeepAlive"];
        // KeepAlive 可能是 bool 或条件字典，两者都表示会重新拉起。
        BOOL configured = [keepAlive isKindOfClass:NSDictionary.class] ||
                          ([keepAlive respondsToSelector:@selector(boolValue)] && [keepAlive boolValue]);
        active = (getppid() == 1) && configured;
    });
    return active;
}

#pragma mark - 运行记录

static char gExitPath[1024];   // 预先算好，信号处理里不能分配内存

// launchd（pid 1）的启动时间：重新越狱是用户态重启，launchd 会重新启动，以此识别「新一轮」。
static long long RKLaunchdStartTime(void) {
    struct kinfo_proc kp;
    size_t len = sizeof(kp);
    int mib[4] = { CTL_KERN, KERN_PROC, KERN_PROC_PID, 1 };
    if (sysctl(mib, 4, &kp, &len, NULL, 0) != 0 || len == 0) return 0;
    return (long long)kp.kp_proc.p_starttime.tv_sec;
}

NSDictionary *RKRunRecordBegin(void) {
    NSString *dir = [RKStatePath() stringByDeletingLastPathComponent];
    NSString *runPath = [dir stringByAppendingPathComponent:@"run.plist"];
    NSString *exitPath = [dir stringByAppendingPathComponent:@"run.exit"];
    strlcpy(gExitPath, exitPath.fileSystemRepresentation, sizeof(gExitPath));

    NSDictionary *prev = [NSDictionary dictionaryWithContentsOfFile:runPath];
    NSString *exit = [[NSString stringWithContentsOfFile:exitPath encoding:NSUTF8StringEncoding error:nil]
                      stringByTrimmingCharactersInSet:NSCharacterSet.whitespaceAndNewlineCharacterSet];
    [[NSFileManager defaultManager] removeItemAtPath:exitPath error:nil];

    long long launchd = RKLaunchdStartTime();
    NSString *version = @(RK_VERSION);
    NSMutableDictionary *out = [NSMutableDictionary dictionary];
    out[@"last_exit"] = ([exit isEqualToString:@"clean"] || [exit isEqualToString:@"watchdog"]) ? exit : @"unknown";
    if (!prev || [prev[@"launchd"] longLongValue] != launchd) {
        out[@"reason"] = @"boot";
    } else if (![prev[@"version"] isEqual:version]) {
        // 版本变低是回滚（OTA 自动回滚或手动降级）。
        NSString *pv = [prev[@"version"] isKindOfClass:NSString.class] ? prev[@"version"] : @"";
        out[@"reason"] = [version compare:pv options:NSNumericSearch] == NSOrderedAscending ? @"downgrade" : @"upgrade";
        out[@"prev_version"] = pv;
    } else if ([exit isEqualToString:@"clean"]) {
        out[@"reason"] = @"restart";   // 正常退出（SIGTERM）后被重新拉起，不算被杀
    } else {
        out[@"reason"] = @"respawn";
    }
    [@{ @"version": version, @"launchd": @(launchd) } writeToFile:runPath atomically:YES];
    return out;
}

void RKRunRecordMarkExit(const char *reason) {
    if (!gExitPath[0]) return;
    int fd = open(gExitPath, O_WRONLY | O_CREAT | O_TRUNC, 0600);
    if (fd < 0) return;
    (void)write(fd, reason, strlen(reason));
    close(fd);
}
