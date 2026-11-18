// RKWatchdog.m — 说明见头文件。

#import "RKWatchdog.h"
#import "RKSystem.h"
#import <os/log.h>
#import <stdatomic.h>
#import <time.h>

static uint64_t RKNowSeconds(void) {
    // uptime 时钟：设备休眠时与队列一起停走，休眠不会被误判为卡死。
    return clock_gettime_nsec_np(CLOCK_UPTIME_RAW) / NSEC_PER_SEC;
}

static _Atomic uint64_t gMainBeat, gWatchedBeat, gLastCheck;
static dispatch_source_t gTimer;   // 用全局变量：ARC 下无引用的 source 会停止触发

void RKWatchdogStart(dispatch_queue_t watched) {
    uint64_t now = RKNowSeconds();
    atomic_store(&gMainBeat, now);
    atomic_store(&gWatchedBeat, now);
    atomic_store(&gLastCheck, now);

    // 独立队列 + timer，watchdog 不依赖被监视对象。leeway 放宽便于系统合并唤醒。
    dispatch_queue_t q = dispatch_queue_create("com.icloudza.residentd.watchdog",
        dispatch_queue_attr_make_with_qos_class(DISPATCH_QUEUE_SERIAL, QOS_CLASS_UTILITY, 0));
    dispatch_source_t t = dispatch_source_create(DISPATCH_SOURCE_TYPE_TIMER, 0, 0, q);
    dispatch_source_set_timer(t,
        dispatch_time(DISPATCH_TIME_NOW, RK_WATCHDOG_INTERVAL_S * NSEC_PER_SEC),
        RK_WATCHDOG_INTERVAL_S * NSEC_PER_SEC, 5 * NSEC_PER_SEC);
    dispatch_source_set_event_handler(t, ^{
        uint64_t now = RKNowSeconds();
        uint64_t mainAge = now - atomic_load(&gMainBeat);
        uint64_t watchedAge = now - atomic_load(&gWatchedBeat);
        if (mainAge > RK_WATCHDOG_TIMEOUT_S || watchedAge > RK_WATCHDOG_TIMEOUT_S) {
            os_log_fault(OS_LOG_DEFAULT, "residentd watchdog: hung (main %llus, state %llus) — "
                         "aborting so launchd respawns us", mainAge, watchedAge);
            RKRunRecordMarkExit("watchdog");
            abort();
        }
        atomic_store(&gLastCheck, now);
        dispatch_async(dispatch_get_main_queue(), ^{ atomic_store(&gMainBeat, RKNowSeconds()); });
        dispatch_async(watched, ^{ atomic_store(&gWatchedBeat, RKNowSeconds()); });
    });
    dispatch_resume(t);
    gTimer = t;
}

BOOL RKWatchdogHealthy(void) {
    if (!gTimer) return NO;
    uint64_t now = RKNowSeconds();
    // 容忍漏一次心跳（leeway / 调度抖动），漏两次即不健康。
    uint64_t limit = 2 * RK_WATCHDOG_INTERVAL_S + 5;
    return now - atomic_load(&gLastCheck) <= limit &&
           now - atomic_load(&gMainBeat) <= limit &&
           now - atomic_load(&gWatchedBeat) <= limit;
}
