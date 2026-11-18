package service

import (
	"context"
	"time"

	"github.com/icloudza/ResidentKit/server/internal/dto"
	"github.com/icloudza/ResidentKit/server/internal/model"
	"github.com/icloudza/ResidentKit/server/internal/pkg/buildinfo"
	"github.com/icloudza/ResidentKit/server/internal/repository"
)

// GatewayTelemetry 由设备网关提供的运行数据。
type GatewayTelemetry interface {
	Telemetry() (connections, banned int, denyHourly []int64, rttHourlyMs []float64)
}

// DashboardService 汇总总览页数据。
type DashboardService struct {
	devices     repository.DeviceRepository
	events      repository.EventRepository
	invocations repository.InvocationRepository
	gateway     GatewayTelemetry
}

func NewDashboardService(r *repository.Set, gw GatewayTelemetry) *DashboardService {
	return &DashboardService{devices: r.Devices, events: r.Events, invocations: r.Invocations, gateway: gw}
}

func (s *DashboardService) Summary(ctx context.Context) (*dto.Summary, error) {
	now := time.Now().UTC()
	dayAgo := now.Add(-24 * time.Hour)
	start := now.Truncate(time.Hour).Add(-23 * time.Hour)
	out := &dto.Summary{ServerVersion: buildinfo.Version, UptimeSeconds: int64(now.Sub(buildinfo.StartedAt).Seconds())}

	devs, err := s.devices.List(ctx)
	if err != nil {
		return nil, err
	}
	var rttSum, rttN int64
	for _, d := range devs {
		out.DevicesTotal++
		if d.Online {
			out.DevicesOnline++
			if d.RTTMs > 0 {
				rttSum += int64(d.RTTMs)
				rttN++
			}
		}
	}
	if rttN > 0 {
		out.AvgRTTMs = rttSum / rttN
	}

	if out.Events24h, err = s.events.CountSince(ctx, dayAgo); err != nil {
		return nil, err
	}
	if out.EventsPrev24h, err = s.events.CountBetween(ctx, dayAgo.Add(-24*time.Hour), dayAgo); err != nil {
		return nil, err
	}
	evHourly, err := s.events.HourlyCounts(ctx, start)
	if err != nil {
		return nil, err
	}
	invHourly, err := s.invocations.HourlyCounts(ctx, start)
	if err != nil {
		return nil, err
	}
	out.EventsHourly = make([]dto.HourCount, 24)
	out.InvocationsHourly = make([]int64, 24)
	for i := 0; i < 24; i++ {
		h := start.Add(time.Duration(i) * time.Hour)
		key := h.Format("2006-01-02T15:00:00Z")
		out.EventsHourly[i] = dto.HourCount{Hour: h, Count: evHourly[key]}
		out.InvocationsHourly[i] = invHourly[key]
	}
	types, err := s.events.TypeCounts(ctx, dayAgo, 6)
	if err != nil {
		return nil, err
	}
	for _, t := range types {
		out.EventTypes = append(out.EventTypes, dto.TypeCount{Type: t.Type, Count: t.Count})
	}

	alertLevels := []string{model.LevelWarning, model.LevelError}
	alerts, total, err := s.events.List(ctx, repository.EventFilter{
		ListQuery: repository.ListQuery{Page: 1, PageSize: 5}, Levels: alertLevels, Since: dayAgo})
	if err != nil {
		return nil, err
	}
	out.Alerts24h = total
	out.RecentAlerts = make([]dto.Event, len(alerts))
	for i := range alerts {
		out.RecentAlerts[i] = dto.EventFrom(&alerts[i])
	}

	st, err := s.invocations.StatusCounts(ctx, dayAgo)
	if err != nil {
		return nil, err
	}
	for status, n := range st {
		out.Invocations24h += n
		switch status {
		case model.InvSucceeded:
			out.InvocationsOK24h += n
			out.InvocationsDone24h += n
		case model.InvPending, model.InvSent:
			out.InvocationsPending += n
		default:
			out.InvocationsDone24h += n
		}
	}

	out.Gateway.Connections, out.Gateway.BannedIPs, out.Gateway.DenyHourly, out.Gateway.RTTHourlyMs = s.gateway.Telemetry()
	return out, nil
}

// EventTypes 某时间之后各事件类型的数量（用于筛选标签）。
func (s *DashboardService) EventTypes(ctx context.Context, since time.Time) ([]dto.TypeCount, error) {
	rows, err := s.events.TypeCounts(ctx, since, 20)
	if err != nil {
		return nil, err
	}
	out := make([]dto.TypeCount, len(rows))
	for i, r := range rows {
		out[i] = dto.TypeCount{Type: r.Type, Count: r.Count}
	}
	return out, nil
}

// DeviceActivity 设备活动概览：range 为 24h（按小时）、7d 或 30d（按天），补齐没有数据的时间桶。
// tzOffset 为浏览器时区相对 UTC 的分钟数（东八区为 480），按天统计时以当地零点分桶。
func (s *DashboardService) DeviceActivity(ctx context.Context, deviceID uint64, rng string, tzOffset int) (*dto.DeviceActivity, error) {
	if tzOffset < -14*60 || tzOffset > 14*60 {
		tzOffset = 0
	}
	loc := time.FixedZone("", tzOffset*60)
	now := time.Now().In(loc)
	daily, n := false, 24
	switch rng {
	case "7d":
		daily, n = true, 7
	case "30d":
		daily, n = true, 30
	default:
		rng = "24h"
	}
	// 桶起点（当地时间）；key 用当地时间的字符串，SQL 里把 created_at 平移到当地时间后同样格式化。
	var first time.Time
	step, layout, format := time.Hour, "2006-01-02 15", "%Y-%m-%d %H"
	if daily {
		step, layout, format = 24*time.Hour, "2006-01-02", "%Y-%m-%d"
		first = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -(n - 1))
	} else {
		first = time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, loc).Add(-time.Duration(n-1) * time.Hour)
	}
	inv, err := s.invocations.DeviceBuckets(ctx, deviceID, first.UTC(), format, tzOffset)
	if err != nil {
		return nil, err
	}
	ev, err := s.events.DeviceBuckets(ctx, deviceID, first.UTC(), format, tzOffset)
	if err != nil {
		return nil, err
	}
	out := &dto.DeviceActivity{Range: rng, Buckets: make([]dto.ActivityBucket, n)}
	idx := map[string]int{}
	for i := range n {
		t := first.Add(time.Duration(i) * step)
		if daily {
			t = first.AddDate(0, 0, i)
		}
		out.Buckets[i].At = t.Format(time.RFC3339)
		idx[t.Format(layout)] = i
	}
	for _, b := range inv {
		if i, ok := idx[b.Bucket]; ok {
			out.Buckets[i].Invocations, out.Buckets[i].Failed = b.Total, b.Bad
			out.Invocations += b.Total
			out.Failed += b.Bad
		}
	}
	for _, b := range ev {
		if i, ok := idx[b.Bucket]; ok {
			out.Buckets[i].Events, out.Buckets[i].Warnings = b.Total, b.Bad
			out.Events += b.Total
			out.Warnings += b.Bad
		}
	}
	return out, nil
}
