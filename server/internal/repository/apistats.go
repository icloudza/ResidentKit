package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/icloudza/ResidentKit/server/internal/model"
)

// APICallFilter 开放 API 调用记录的筛选。
type APICallFilter struct {
	ListQuery
	KeyID    uint64   // 0 表示全部密钥
	DeviceID uint64   // 0 表示全部设备
	Statuses []string // 为空表示不限
	Since    time.Time
}

// APIHour 某小时某密钥的调用数（KeyID 为 0 表示全部密钥合计）。
type APIHour struct {
	KeyID uint64
	Hour  string // 2006-01-02T15:00:00Z
	Total int64
	OK    int64
	Bad   int64 // failed / lost / expired
}

// CommandCount 命令调用次数。
type CommandCount struct {
	Module  string
	Command string
	N       int64
}

// APIStatsRepository 开放 API 的调用记录与统计（数据来自 invocations 中 api_key_id 非空的行）。
type APIStatsRepository interface {
	List(ctx context.Context, f APICallFilter) ([]model.Invocation, int64, error)
	Hourly(ctx context.Context, since time.Time) ([]APIHour, error)
	StatusCounts(ctx context.Context, since time.Time) (map[string]int64, error)
	Count(ctx context.Context, from, to time.Time) (int64, error)
	TopCommands(ctx context.Context, since time.Time, limit int) ([]CommandCount, error)
	// Latency 成功调用从下发到结果的耗时（毫秒）：平均值与 p95。
	Latency(ctx context.Context, since time.Time) (avg, p95 float64, err error)
}

type apiStatsRepo struct{ db *gorm.DB }

func (r *apiStatsRepo) base(ctx context.Context) *gorm.DB {
	return r.db.WithContext(ctx).Model(&model.Invocation{}).Where("api_key_id IS NOT NULL")
}

func (r *apiStatsRepo) List(ctx context.Context, f APICallFilter) ([]model.Invocation, int64, error) {
	tx := r.base(ctx)
	if f.KeyID != 0 {
		tx = tx.Where("api_key_id = ?", f.KeyID)
	}
	if f.DeviceID != 0 {
		tx = tx.Where("device_id = ?", f.DeviceID)
	}
	if len(f.Statuses) > 0 {
		tx = tx.Where("status IN ?", f.Statuses)
	}
	if !f.Since.IsZero() {
		tx = tx.Where("created_at >= ?", f.Since)
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var out []model.Invocation
	err := tx.Order("id DESC").Offset(f.offset()).Limit(f.Size()).Find(&out).Error
	return out, total, err
}

func (r *apiStatsRepo) Hourly(ctx context.Context, since time.Time) ([]APIHour, error) {
	var rows []APIHour
	err := r.base(ctx).Select(`api_key_id AS key_id, DATE_FORMAT(created_at, '%Y-%m-%dT%H:00:00Z') AS hour, COUNT(*) AS total,
		SUM(status = 'succeeded') AS ok, SUM(status IN ('failed','lost','expired')) AS bad`).
		Where("created_at >= ?", since).Group("api_key_id, hour").Scan(&rows).Error
	return rows, err
}

func (r *apiStatsRepo) StatusCounts(ctx context.Context, since time.Time) (map[string]int64, error) {
	type row struct {
		Status string
		N      int64
	}
	var rows []row
	err := r.base(ctx).Select("status, COUNT(*) AS n").Where("created_at >= ?", since).Group("status").Scan(&rows).Error
	out := make(map[string]int64, len(rows))
	for _, x := range rows {
		out[x.Status] = x.N
	}
	return out, err
}

func (r *apiStatsRepo) Count(ctx context.Context, from, to time.Time) (int64, error) {
	var n int64
	return n, r.base(ctx).Where("created_at >= ? AND created_at < ?", from, to).Count(&n).Error
}

func (r *apiStatsRepo) TopCommands(ctx context.Context, since time.Time, limit int) ([]CommandCount, error) {
	var rows []CommandCount
	err := r.base(ctx).Select("module, command, COUNT(*) AS n").Where("created_at >= ?", since).
		Group("module, command").Order("n DESC").Limit(limit).Scan(&rows).Error
	return rows, err
}

func (r *apiStatsRepo) latencyScope(ctx context.Context, since time.Time) *gorm.DB {
	return r.base(ctx).Where("created_at >= ? AND status = ? AND sent_at IS NOT NULL AND finished_at IS NOT NULL",
		since, model.InvSucceeded)
}

const latencyExpr = "TIMESTAMPDIFF(MICROSECOND, sent_at, finished_at) / 1000"

func (r *apiStatsRepo) Latency(ctx context.Context, since time.Time) (float64, float64, error) {
	var agg struct {
		N   int64
		Avg float64
	}
	if err := r.latencyScope(ctx, since).Select("COUNT(*) AS n, COALESCE(AVG(" + latencyExpr + "), 0) AS avg").
		Scan(&agg).Error; err != nil || agg.N == 0 {
		return 0, 0, err
	}
	p95, err := apiStatsLatencyP95(r, ctx, since, agg.N)
	return agg.Avg, p95, err
}

// apiStatsLatencyP95 共 n 条成功调用时的第 95 百分位耗时。
func apiStatsLatencyP95(r *apiStatsRepo, ctx context.Context, since time.Time, n int64) (float64, error) {
	var p95 float64
	err := r.latencyScope(ctx, since).Select(latencyExpr + " AS d").Order("d").
		Offset(int(float64(n-1) * 0.95)).Limit(1).Scan(&p95).Error
	return p95, err
}
