// RKWatchdog.h — 把卡死变成重启。
//
// 主队列或被监视队列超过 RK_WATCHDOG_TIMEOUT_S 无进展，说明进程卡死（死锁、系统调用
// 阻塞……）。进程仍存活，launchd 察觉不到，所以主动 abort() 交给 KeepAlive 重新拉起；
// crash report 会保留卡住线程的调用栈。

#import <Foundation/Foundation.h>

#define RK_WATCHDOG_INTERVAL_S  15
#define RK_WATCHDOG_TIMEOUT_S   60

void RKWatchdogStart(dispatch_queue_t watchedQueue);

// watchdog 已启动且两个队列近期都有响应时为 YES。
BOOL RKWatchdogHealthy(void);
