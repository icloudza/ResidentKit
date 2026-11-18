package gateway

import (
	"sync"
	"time"
)

// hourly 最近 24 小时按小时聚合的计数 / 均值，只在内存中（进程重启后清零）。
type hourly struct {
	mu      sync.Mutex
	buckets [24]bucket
}

type bucket struct {
	hour  int64 // Unix 小时数
	count int64
	sum   float64
}

func (h *hourly) add(v float64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	hr := time.Now().Unix() / 3600
	b := &h.buckets[hr%24]
	if b.hour != hr {
		*b = bucket{hour: hr}
	}
	b.count++
	b.sum += v
}

// Series 从 23 小时前到当前小时依次返回 (计数, 均值)。
func (h *hourly) series() (counts []int64, avgs []float64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := time.Now().Unix() / 3600
	counts, avgs = make([]int64, 24), make([]float64, 24)
	for i := 0; i < 24; i++ {
		hr := now - 23 + int64(i)
		if b := h.buckets[hr%24]; b.hour == hr && b.count > 0 {
			counts[i], avgs[i] = b.count, b.sum/float64(b.count)
		}
	}
	return
}
