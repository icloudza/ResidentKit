// RKMemorystatus.h — 用到的 XNU 私有 memorystatus（jetsam）API。
// iOS SDK 未公开，取值来自 xnu/bsd/sys/kern_memorystatus.h。

#ifndef RK_MEMORYSTATUS_H
#define RK_MEMORYSTATUS_H

#include <stdint.h>
#include <sys/types.h>
#include <sys/resource.h>

int memorystatus_control(uint32_t command, int32_t pid, uint32_t flags,
                         void *buffer, size_t buffersize);
int proc_pid_rusage(int pid, int flavor, rusage_info_t *buffer);

#define MEMORYSTATUS_CMD_GET_PRIORITY_LIST          1
#define MEMORYSTATUS_CMD_SET_PRIORITY_PROPERTIES    2
#define MEMORYSTATUS_CMD_GET_MEMLIMIT_PROPERTIES    8

// jetsam band（新版 ×10 刻度），越低越先被杀。
#define RK_JETSAM_BAND_BACKGROUND   40   // launchd 给第三方 daemon 的默认值
#define RK_JETSAM_BAND_FOREGROUND   100  // 前台 app
#define RK_JETSAM_BAND_SYSTEM       140  // 多数 Apple daemon 所在档位（cfprefsd、locationd）

typedef struct {
    pid_t    pid;
    int32_t  priority;
    uint64_t user_data;
    int32_t  limit;
    uint32_t state;
} rk_memorystatus_priority_entry_t;

typedef struct {
    int32_t  priority;
    uint64_t user_data;
} rk_memorystatus_priority_properties_t;

typedef struct {
    int32_t  memlimit_active;
    uint32_t memlimit_active_attr;
    int32_t  memlimit_inactive;
    uint32_t memlimit_inactive_attr;
} rk_memorystatus_memlimit_t;

#endif /* RK_MEMORYSTATUS_H */
