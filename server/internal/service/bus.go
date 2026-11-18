package service

import (
	"sync"
)

// 推送给控制台的实时消息类型。
const (
	TopicDeviceOnline      = "device.online"
	TopicDeviceOffline     = "device.offline"
	TopicDeviceUpdated     = "device.updated"
	TopicDeviceDeleted     = "device.deleted"
	TopicEventCreated      = "event.created"
	TopicInvocationUpdated = "invocation.updated"
	TopicUpdateChanged     = "update.changed" // 设备更新状态变化（OTA）
	TopicConfigChanged     = "config.changed" // 设备配置或同步状态变化
	TopicEnrollChanged     = "enroll.changed" // 设备接入申请变化
)

type BusMessage struct {
	Topic string `json:"topic"`
	Data  any    `json:"data"`
}

// Bus 进程内发布订阅，供 SSE 推送。订阅者处理过慢时丢弃消息，不阻塞发布方。
type Bus struct {
	mu   sync.RWMutex
	subs map[chan BusMessage]struct{}
}

func NewBus() *Bus { return &Bus{subs: make(map[chan BusMessage]struct{})} }

func (b *Bus) Publish(topic string, data any) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.subs {
		select {
		case ch <- BusMessage{Topic: topic, Data: data}:
		default: // 订阅者积压，丢弃
		}
	}
}

// Subscribe 返回消息通道和取消函数。
func (b *Bus) Subscribe() (<-chan BusMessage, func()) {
	ch := make(chan BusMessage, 64)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		if _, ok := b.subs[ch]; ok {
			delete(b.subs, ch)
			close(ch)
		}
		b.mu.Unlock()
	}
}
