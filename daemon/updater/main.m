// rk-updater — OTA 安装助手，由 system.update 模块以一次性 launchd 任务（launchctl bootstrap）启动。
//
// 作为独立的 launchd 任务运行，安装期间 daemon 被 bootout 也不受影响。流程：
//   dpkg -i 新包 → 健康检查（90 秒内新 daemon 可连通、版本正确且 20 秒内未重启）
//   → 失败则 dpkg -i 缓存的上一版（current.deb）回滚 → 写 result.plist、发 Darwin 通知
//   → 成功则把新包缓存为 current.deb → 退出（LaunchOnlyOnce，launchd 自动卸载）。
// 任务文件先改名占用：即使被意外重新拉起也只执行一次，并能报告「被中断」。

#import <Foundation/Foundation.h>
#import <notify.h>
#import <spawn.h>
#import <sys/stat.h>
#import <sys/wait.h>
#import "../../common/RKProtocol.h"
#import "../../common/RKXPCCompat.h"

extern char **environ;

static const double kHealthDeadline = 90;   // 秒
static const double kStableFor = 20;        // 秒

static NSString *gRoot;
static NSFileHandle *gLog;

static void Log(NSString *fmt, ...) NS_FORMAT_FUNCTION(1, 2);
static void Log(NSString *fmt, ...) {
    va_list ap;
    va_start(ap, fmt);
    NSString *s = [[NSString alloc] initWithFormat:fmt arguments:ap];
    va_end(ap);
    NSString *line = [NSString stringWithFormat:@"%@ %@\n", [NSDate date], s];
    [gLog writeData:[line dataUsingEncoding:NSUTF8StringEncoding]];
}

// 运行命令（带越狱 PATH），返回退出码。
static int Run(NSArray<NSString *> *argv) {
    NSString *path = [NSString stringWithFormat:@"PATH=%1$@/usr/local/sbin:%1$@/usr/local/bin:%1$@/usr/sbin:%1$@/usr/bin:%1$@/sbin:%1$@/bin:/usr/sbin:/usr/bin:/sbin:/bin", gRoot];
    char *env[] = { (char *)path.UTF8String, "DEBIAN_FRONTEND=noninteractive", "LANG=C", NULL };
    char **args = calloc(argv.count + 1, sizeof(char *));
    for (NSUInteger i = 0; i < argv.count; i++) args[i] = (char *)argv[i].UTF8String;
    pid_t pid;
    int rc = posix_spawn(&pid, args[0], NULL, NULL, args, env);
    free(args);
    if (rc != 0) { Log(@"spawn %@ failed: %d", argv[0], rc); return -1; }
    int st = 0;
    waitpid(pid, &st, 0);
    int code = WIFEXITED(st) ? WEXITSTATUS(st) : -1;
    Log(@"%@ → %d", [argv componentsJoinedByString:@" "], code);
    return code;
}

static int Dpkg(NSString *deb) {
    return Run(@[ [gRoot stringByAppendingString:@"/usr/bin/dpkg"], @"-i", deb ]);
}

// 经 XPC ping daemon，返回 (version, pid)；连不上返回 nil。
static NSDictionary *Ping(void) {
    NSXPCConnection *c = RKConnectionToMachService(@(RK_MACH_SERVICE), NSXPCConnectionPrivileged);
    c.remoteObjectInterface = RKDaemonInterface();
    [c resume];
    __block NSDictionary *out = nil;
    dispatch_semaphore_t done = dispatch_semaphore_create(0);
    id<RKDaemonProtocol> d = [c remoteObjectProxyWithErrorHandler:^(NSError *e) { dispatch_semaphore_signal(done); }];
    [d pingWithReply:^(NSString *version, uint64_t pid) {
        out = @{ @"version": version ?: @"", @"pid": @(pid) };
        dispatch_semaphore_signal(done);
    }];
    dispatch_semaphore_wait(done, dispatch_time(DISPATCH_TIME_NOW, 3 * NSEC_PER_SEC));
    [c invalidate];
    return out;
}

// 新版本可连通、版本号正确，并在 kStableFor 秒内没有重启（pid 不变）。
static BOOL Healthy(NSString *version) {
    NSDate *deadline = [NSDate dateWithTimeIntervalSinceNow:kHealthDeadline];
    while (deadline.timeIntervalSinceNow > 0) {
        NSDictionary *a = Ping();
        if (a && [a[@"version"] isEqualToString:version]) {
            [NSThread sleepForTimeInterval:kStableFor];
            NSDictionary *b = Ping();
            if (b && [b[@"version"] isEqualToString:version] && [b[@"pid"] isEqual:a[@"pid"]]) {
                Log(@"healthy: %@ pid %@", version, b[@"pid"]);
                return YES;
            }
            Log(@"unstable: pid %@ → %@", a[@"pid"], b[@"pid"] ?: @"(down)");
        }
        [NSThread sleepForTimeInterval:3];
    }
    Log(@"not healthy within %.0fs", kHealthDeadline);
    return NO;
}

static void Finish(NSString *dir, NSDictionary *result) {
    if (result) {
        [result writeToFile:[dir stringByAppendingPathComponent:@"result.plist"] atomically:YES];
        notify_post("com.icloudza.residentd.update-result");
        Log(@"result: %@", result[@"status"]);
    }
    exit(0);   // LaunchOnlyOnce：退出后 launchd 自动卸载本任务
}

int main(int argc, char *argv[]) {
    @autoreleasepool {
        umask(077);
        const char *jb = getenv("JBROOT");
        gRoot = jb ? @(jb) : @"/var/jb";
        char real[PATH_MAX];   // 真实路径，不依赖 /var/jb 链接（见 RKSystem.m）
        if (gRoot.length && realpath(gRoot.fileSystemRepresentation, real)) gRoot = @(real);
        if (argc < 2) return 64;
        NSString *jobPath = @(argv[1]);
        NSString *dir = [jobPath stringByDeletingLastPathComponent];
        NSString *running = [jobPath stringByAppendingString:@".running"];

        NSString *logPath = [dir stringByAppendingPathComponent:@"updater.log"];
        if (![[NSFileManager defaultManager] fileExistsAtPath:logPath])
            [[NSFileManager defaultManager] createFileAtPath:logPath contents:nil attributes:nil];
        gLog = [NSFileHandle fileHandleForWritingAtPath:logPath];
        [gLog seekToEndOfFile];

        NSFileManager *fm = [NSFileManager defaultManager];
        if (![fm fileExistsAtPath:jobPath]) {
            // 被 launchd 重新拉起：若上次中途被打断，报告失败；否则直接注销。
            NSDictionary *half = [NSDictionary dictionaryWithContentsOfFile:running];
            [fm removeItemAtPath:running error:nil];
            Finish(dir, half ? @{ @"status": @"failed", @"version": half[@"version"] ?: @"",
                                  @"release_id": half[@"release_id"] ?: @0, @"from": half[@"from"] ?: @"",
                                  @"error": @"更新程序被中断" } : nil);
        }
        [fm removeItemAtPath:running error:nil];
        [fm moveItemAtPath:jobPath toPath:running error:nil];
        NSDictionary *job = [NSDictionary dictionaryWithContentsOfFile:running];
        NSString *deb = job[@"deb"], *version = job[@"version"];
        NSString *current = [dir stringByAppendingPathComponent:@"current.deb"];
        NSMutableDictionary *result = [@{ @"version": version ?: @"", @"release_id": job[@"release_id"] ?: @0,
                                          @"from": job[@"from"] ?: @"" } mutableCopy];
        Log(@"=== install %@ (from %@)", version, job[@"from"]);

        int rc = Dpkg(deb);
        if (rc == 0 && Healthy(version)) {
            [fm removeItemAtPath:current error:nil];
            [fm moveItemAtPath:deb toPath:current error:nil];   // 缓存为下次回滚的版本
            [version writeToFile:[dir stringByAppendingPathComponent:@"current.version"] atomically:YES
                        encoding:NSUTF8StringEncoding error:nil];
            result[@"status"] = @"installed";
        } else {
            result[@"error"] = rc != 0 ? [NSString stringWithFormat:@"dpkg 安装失败（%d）", rc] : @"新版本启动后不健康";
            if ([fm fileExistsAtPath:current] && Dpkg(current) == 0) {
                result[@"status"] = @"rolled_back";
            } else {
                result[@"status"] = @"failed";
                if (![fm fileExistsAtPath:current]) result[@"error"] = [result[@"error"] stringByAppendingString:@"，且没有可回滚的版本"];
            }
            [fm removeItemAtPath:deb error:nil];
        }
        [fm removeItemAtPath:running error:nil];
        Finish(dir, result);
    }
    return 0;
}
