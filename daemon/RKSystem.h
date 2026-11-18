// RKSystem.h — 进程级工具：越狱路径、jetsam、launchd 检查。

#import <Foundation/Foundation.h>

NS_ASSUME_NONNULL_BEGIN

// 越狱根目录，取自 launchd plist 传入的 JBROOT 变量。
// 未设置 → Dopamine 默认 /var/jb；设置为空 → rootful（""）。
NSString *RKJailbreakRoot(void);
// Dopamine「隐藏越狱」中（/var/jb 链接被删除）。此时 dpkg 等越狱工具不可用。
BOOL RKJailbreakHidden(void);

// <jbroot>/var/lib/residentd/state.plist
NSString *RKStatePath(void);

// 把本进程移到系统 daemon 的 jetsam band（140），需要 com.apple.private.memorystatus。
void RKRaiseJetsamBand(void);

uint64_t RKFootprintBytes(void);

// 设备信息 {model: 机型标识如 iPhone12,1, os: "iOS 16.7.10", app_version: App 的 CFBundleShortVersionString}，
// 取不到的项为空字符串。随 hello 上报，也是 net.link/info 的一部分。
NSDictionary<NSString *, NSString *> *RKDeviceInfo(void);

// 运行记录：判断本次启动原因，供 system.daemon.started 事件使用。
// 返回 {reason: boot|upgrade|restart|respawn, last_exit: clean|watchdog|unknown, prev_version?}，
// 并把本次记为「运行中」。启动时调用一次。
NSDictionary *RKRunRecordBegin(void);

// 记下退出原因（"clean" / "watchdog"），只用 open/write，可在信号处理和 abort 前调用。
void RKRunRecordMarkExit(const char *reason);

// 由 launchd（ppid 1）按带 KeepAlive 的 plist 拉起时为 YES，即退出后会被重新拉起。
// 仅在启动时判定一次。
BOOL RKKeepAliveActive(void);

NS_ASSUME_NONNULL_END
