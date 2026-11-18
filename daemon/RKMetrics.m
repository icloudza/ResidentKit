// RKMetrics.m — 供 queryMetrics 使用的按需资源采样。
//
// 不挂定时器：只在客户端请求时采样，没开 UI 时指标开销为零。
//
// 长期运行注意（app 打开期间约每秒调用一次）：不泄漏 Mach port（缓存
// mach_host_self、释放 thread port），不堆积 autorelease（调用方包了 pool）。

#import "RKMetrics.h"
#import "../common/RKProtocol.h"
#import "../common/RKMemorystatus.h"
#import <mach/mach.h>
#import <sys/sysctl.h>
#import <time.h>

static double RKSeconds(struct timeval tv) {
    return (double)tv.tv_sec + tv.tv_usec / 1e6;
}

static uint64_t RKSysctlU64(const char *name) {
    uint64_t v = 0; size_t len = sizeof(v);
    if (sysctlbyname(name, &v, &len, NULL, 0) != 0) return 0;
    return len == sizeof(uint32_t) ? (uint64_t)*(uint32_t *)&v : v;
}

static int RKThreadCount(void) {
    thread_act_array_t threads = NULL;
    mach_msg_type_number_t n = 0;
    if (task_threads(mach_task_self(), &threads, &n) != KERN_SUCCESS) return 0;
    for (mach_msg_type_number_t i = 0; i < n; i++) {
        mach_port_deallocate(mach_task_self(), threads[i]);
    }
    vm_deallocate(mach_task_self(), (vm_address_t)threads, n * sizeof(thread_act_t));
    return (int)n;
}

NSDictionary *RKSampleMetrics(void) {
    static mach_port_t host;
    static BOOL haveLast;
    static double lastWall, lastSelfCPU;
    static host_cpu_load_info_data_t lastLoad;
    if (!host) host = mach_host_self();

    NSMutableDictionary *m = [NSMutableDictionary dictionaryWithCapacity:12];

    // ---- 服务：内存 -------------------------------------------------------
    struct rusage_info_v4 ru = {0};
    if (proc_pid_rusage(getpid(), RUSAGE_INFO_V4, (rusage_info_t *)&ru) == 0) {
        m[RKMetricServiceFootprint] = @(ru.ri_phys_footprint);
        m[RKMetricServicePeak]      = @(ru.ri_lifetime_max_phys_footprint);
    }
    rk_memorystatus_memlimit_t ml = {0};
    if (memorystatus_control(MEMORYSTATUS_CMD_GET_MEMLIMIT_PROPERTIES, getpid(), 0,
                             &ml, sizeof(ml)) == 0 && ml.memlimit_active > 0) {
        m[RKMetricServiceLimit] = @((uint64_t)ml.memlimit_active * 1024 * 1024);
    } else {
        m[RKMetricServiceLimit] = @0;
    }
    rk_memorystatus_priority_entry_t pe = {0};
    if (memorystatus_control(MEMORYSTATUS_CMD_GET_PRIORITY_LIST, getpid(), 0,
                             &pe, sizeof(pe)) > 0) {
        m[RKMetricJetsamBand] = @(pe.priority);
    }
    m[RKMetricServiceThreads] = @(RKThreadCount());

    // ---- CPU（服务 + 设备），取增量 --------------------------------------
    double wall = clock_gettime_nsec_np(CLOCK_UPTIME_RAW) / 1e9;
    struct rusage self = {0};
    getrusage(RUSAGE_SELF, &self);
    double selfCPU = RKSeconds(self.ru_utime) + RKSeconds(self.ru_stime);

    host_cpu_load_info_data_t load = {0};
    mach_msg_type_number_t cnt = HOST_CPU_LOAD_INFO_COUNT;
    BOOL haveLoad = host_statistics(host, HOST_CPU_LOAD_INFO,
                                    (host_info_t)&load, &cnt) == KERN_SUCCESS;

    double svcCPU = 0, devCPU = 0;
    if (haveLast && wall > lastWall) {
        svcCPU = (selfCPU - lastSelfCPU) / (wall - lastWall) * 100.0;
    } else if (wall > 0) {
        svcCPU = 0; // 首次调用：还没有区间
    }
    if (haveLoad) {
        uint64_t busy = 0, total = 0;
        for (int s = 0; s < CPU_STATE_MAX; s++) {
            uint64_t d = haveLast ? (uint64_t)(load.cpu_ticks[s] - lastLoad.cpu_ticks[s])
                                  : load.cpu_ticks[s];
            total += d;
            if (s != CPU_STATE_IDLE) busy += d;
        }
        devCPU = total ? (double)busy / (double)total * 100.0 : 0;
        lastLoad = load;
    }
    lastWall = wall; lastSelfCPU = selfCPU; haveLast = YES;

    m[RKMetricServiceCPU] = @(MAX(0.0, MIN(100.0, svcCPU)));
    m[RKMetricDeviceCPU]  = @(MAX(0.0, MIN(100.0, devCPU)));
    m[RKMetricDeviceCores] = @(RKSysctlU64("hw.ncpu"));

    // ---- 设备：内存 -------------------------------------------------------
    m[RKMetricDeviceMemTotal] = @(RKSysctlU64("hw.memsize"));
    vm_statistics64_data_t vm = {0};
    mach_msg_type_number_t vcnt = HOST_VM_INFO64_COUNT;
    if (host_statistics64(host, HOST_VM_INFO64, (host_info64_t)&vm, &vcnt) == KERN_SUCCESS) {
        uint64_t page = vm_kernel_page_size;
        uint64_t used = ((uint64_t)vm.active_count + vm.wire_count +
                         vm.compressor_page_count) * page;
        m[RKMetricDeviceMemUsed] = @(used);
    }
    int pressure = 0; size_t plen = sizeof(pressure);
    if (sysctlbyname("kern.memorystatus_vm_pressure_level", &pressure, &plen, NULL, 0) == 0) {
        m[RKMetricMemoryPressure] = @(pressure);
    }

    return m;
}
