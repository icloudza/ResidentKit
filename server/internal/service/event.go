package service

import (
	"context"
	"encoding/json"
	"time"

	"github.com/icloudza/ResidentKit/server/internal/dto"
	"github.com/icloudza/ResidentKit/server/internal/model"
	"github.com/icloudza/ResidentKit/server/internal/repository"
)

type EventService struct {
	repo  repository.EventRepository
	bus   *Bus
	hooks []EventHook
}

// EventHook 新事件落库后的回调（如 OTA 根据 system.update.* 更新状态）。
type EventHook func(ctx context.Context, deviceID uint64, typ string, payload json.RawMessage)

// AddHook 装配时添加，运行中不再修改。
func (s *EventService) AddHook(h EventHook) { s.hooks = append(s.hooks, h) }

func NewEventService(r *repository.Set, bus *Bus) *EventService {
	return &EventService{repo: r.Events, bus: bus}
}

// Ingest 写入设备上报的事件。先落库再由网关回 ack，保证确认即持久；重复 id 直接忽略。
func (s *EventService) Ingest(ctx context.Context, deviceID uint64, msgID, typ, level string, payload json.RawMessage, deviceTS float64) error {
	e := &model.Event{DeviceID: deviceID, MsgID: trunc(msgID, 64), Type: trunc(typ, 64), Level: model.ValidLevel(level),
		CreatedAt: time.Now().UTC()}
	if len(payload) > 0 && string(payload) != "null" {
		e.Payload = []byte(payload)
	}
	if deviceTS > 0 {
		t := time.Unix(0, int64(deviceTS*1e9)).UTC()
		e.DeviceTS = &t
	}
	inserted, err := s.repo.InsertIgnore(ctx, e)
	if err == nil && inserted {
		s.bus.Publish(TopicEventCreated, dto.EventFrom(e))
		for _, h := range s.hooks {
			h(ctx, deviceID, e.Type, payload)
		}
	}
	return err
}

func (s *EventService) List(ctx context.Context, f repository.EventFilter) (dto.Page[dto.Event], error) {
	rows, total, err := s.repo.List(ctx, f)
	if err != nil {
		return dto.Page[dto.Event]{}, err
	}
	out := make([]dto.Event, len(rows))
	for i := range rows {
		out[i] = dto.EventFrom(&rows[i])
	}
	return dto.Page[dto.Event]{Items: out, Total: total, Page: f.Num(), PageSize: f.Size()}, nil
}

// Hourly 近 24 小时每小时事件数（含 0）。
func (s *EventService) Hourly(ctx context.Context) ([]dto.HourCount, int64, error) {
	now := time.Now().UTC()
	start := now.Truncate(time.Hour).Add(-23 * time.Hour)
	counts, err := s.repo.HourlyCounts(ctx, start)
	if err != nil {
		return nil, 0, err
	}
	out := make([]dto.HourCount, 24)
	var total int64
	for i := 0; i < 24; i++ {
		h := start.Add(time.Duration(i) * time.Hour)
		n := counts[h.Format("2006-01-02T15:00:00Z")]
		out[i] = dto.HourCount{Hour: h, Count: n}
		total += n
	}
	return out, total, nil
}
