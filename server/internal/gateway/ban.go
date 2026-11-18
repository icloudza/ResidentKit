package gateway

import (
	"sync"
	"time"
)

// banList 按 IP 统计鉴权失败，窗口内失败过多则封禁一段时间。
type banList struct {
	mu       sync.Mutex
	after    int
	window   time.Duration
	duration time.Duration
	failures map[string][]time.Time
	banned   map[string]time.Time
}

func newBanList(after int, window, duration time.Duration) *banList {
	return &banList{after: after, window: window, duration: duration,
		failures: map[string][]time.Time{}, banned: map[string]time.Time{}}
}

func (b *banList) isBanned(ip string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	until, ok := b.banned[ip]
	if ok && time.Now().Before(until) {
		return true
	}
	delete(b.banned, ip)
	return false
}

// fail 记一次失败，返回是否因此被封禁。
func (b *banList) fail(ip string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	list := b.failures[ip][:0]
	for _, t := range b.failures[ip] {
		if now.Sub(t) < b.window {
			list = append(list, t)
		}
	}
	list = append(list, now)
	if len(list) >= b.after {
		delete(b.failures, ip)
		b.banned[ip] = now.Add(b.duration)
		return true
	}
	b.failures[ip] = list
	return false
}

func (b *banList) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := 0
	for _, until := range b.banned {
		if time.Now().Before(until) {
			n++
		}
	}
	return n
}

// gc 清理过期记录，防止内存增长。
func (b *banList) gc() {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	for ip, until := range b.banned {
		if now.After(until) {
			delete(b.banned, ip)
		}
	}
	for ip, list := range b.failures {
		if len(list) == 0 || now.Sub(list[len(list)-1]) > b.window {
			delete(b.failures, ip)
		}
	}
}
