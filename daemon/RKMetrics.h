// RKMetrics.h — 供 queryMetrics 使用的按需资源采样。

#import <Foundation/Foundation.h>

// 返回以 RKProtocol.h 中 RKMetric* 为 key 的字典。
// CPU 百分比是相对上一次调用的增量（首次调用为自进程启动/开机以来的平均值）。
// 非线程安全：只能在同一个串行队列上调用（residentd 用 state queue）。
NSDictionary *RKSampleMetrics(void);
