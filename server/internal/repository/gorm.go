package repository

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/icloudza/ResidentKit/server/internal/model"
)

// ---- 设备 ----

type deviceRepo struct{ db *gorm.DB }

func (r *deviceRepo) UpsertOnHello(ctx context.Context, d *model.Device) error {
	err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "device_uid"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"name", "version", "online", "connected_at", "last_ip", "network", "remote_commands",
			"config_schema", "config_version", "command_schema", "info", "last_seen_at", "updated_at",
		}),
	}).Create(d).Error
	if err != nil {
		return err
	}
	// MySQL upsert 拿不到已有行的 id，重新查一次。
	return r.db.WithContext(ctx).Where("device_uid = ?", d.DeviceUID).Take(d).Error
}

func (r *deviceRepo) SetConfigVersion(ctx context.Context, id uint64, v int64) error {
	return r.db.WithContext(ctx).Model(&model.Device{}).Where("id = ?", id).Update("config_version", v).Error
}

func (r *deviceRepo) SetOffline(ctx context.Context, id uint64, at time.Time) error {
	return r.db.WithContext(ctx).Model(&model.Device{}).Where("id = ?", id).
		Updates(map[string]any{"online": false, "last_seen_at": at}).Error
}

// MarkAllOffline 先用索引取出在线设备的 id 再按主键更新：直接 UPDATE ... WHERE online = 1
// 时 MySQL 不会用正在被修改的列上的索引，只能扫全表。
func (r *deviceRepo) MarkAllOffline(ctx context.Context) error {
	var ids []uint64
	if err := r.db.WithContext(ctx).Model(&model.Device{}).Where("online = ?", true).Pluck("id", &ids).Error; err != nil {
		return err
	}
	for start := 0; start < len(ids); start += 1000 {
		batch := ids[start:min(start+1000, len(ids))]
		if err := r.db.WithContext(ctx).Model(&model.Device{}).Where("id IN ?", batch).
			Update("online", false).Error; err != nil {
			return err
		}
	}
	return nil
}

func (r *deviceRepo) Touch(ctx context.Context, id uint64, rttMs int, at time.Time) error {
	return r.db.WithContext(ctx).Model(&model.Device{}).Where("id = ?", id).
		Updates(map[string]any{"rtt_ms": rttMs, "last_seen_at": at}).Error
}

func (r *deviceRepo) UpdateMetrics(ctx context.Context, id uint64, metrics []byte, at time.Time) error {
	return r.db.WithContext(ctx).Model(&model.Device{}).Where("id = ?", id).
		Updates(map[string]any{"metrics": metrics, "metrics_at": at, "last_seen_at": at}).Error
}

func (r *deviceRepo) UpdateInfo(ctx context.Context, id uint64, info []byte) error {
	return r.db.WithContext(ctx).Model(&model.Device{}).Where("id = ?", id).Update("info", info).Error
}

func (r *deviceRepo) Delete(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Delete(&model.Device{}, id).Error
}

func (r *deviceRepo) Get(ctx context.Context, id uint64) (*model.Device, error) {
	var d model.Device
	if err := r.db.WithContext(ctx).Take(&d, id).Error; err != nil {
		return nil, notFound(err)
	}
	return &d, nil
}

// List 先在覆盖索引 (online, last_seen_at) 上排好序取 id，再按主键取整行：
// 设备行含较大的 JSON 列，直接 ORDER BY 会全表扫描并对整行排序。
func (r *deviceRepo) List(ctx context.Context) ([]model.Device, error) {
	var ids []uint64
	if err := r.db.WithContext(ctx).Model(&model.Device{}).Order("online DESC, last_seen_at DESC").
		Limit(1000).Pluck("id", &ids).Error; err != nil || len(ids) == 0 {
		return nil, err
	}
	var rows []model.Device
	if err := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, err
	}
	pos := make(map[uint64]int, len(ids))
	for i, id := range ids {
		pos[id] = i
	}
	out := make([]model.Device, len(ids))
	n := 0
	for _, d := range rows {
		if i, ok := pos[d.ID]; ok {
			out[i] = d
			n++
		}
	}
	if n != len(ids) { // 两次查询之间有设备被删除：去掉空位
		kept := out[:0]
		for _, d := range out {
			if d.ID != 0 {
				kept = append(kept, d)
			}
		}
		out = kept
	}
	return out, nil
}

func (r *deviceRepo) SetAlias(ctx context.Context, id uint64, alias string) error {
	res := r.db.WithContext(ctx).Model(&model.Device{}).Where("id = ?", id).Update("alias", alias)
	if res.Error == nil && res.RowsAffected == 0 {
		return ErrNotFound
	}
	return res.Error
}

func (r *deviceRepo) CountOnline(ctx context.Context) (online, total int64, err error) {
	if err = r.db.WithContext(ctx).Model(&model.Device{}).Count(&total).Error; err != nil {
		return
	}
	err = r.db.WithContext(ctx).Model(&model.Device{}).Where("online = ?", true).Count(&online).Error
	return
}

// ---- 设备令牌 ----

type deviceTokenRepo struct{ db *gorm.DB }

func (r *deviceTokenRepo) Create(ctx context.Context, t *model.DeviceToken) error {
	return r.db.WithContext(ctx).Create(t).Error
}

func (r *deviceTokenRepo) FindByHash(ctx context.Context, hash string) (*model.DeviceToken, error) {
	var t model.DeviceToken
	err := r.db.WithContext(ctx).Where("token_hash = ?", hash).Take(&t).Error
	if err != nil {
		return nil, notFound(err)
	}
	return &t, nil
}

func (r *deviceTokenRepo) FindActiveByHash(ctx context.Context, hash string) (*model.DeviceToken, error) {
	var t model.DeviceToken
	err := r.db.WithContext(ctx).Where("token_hash = ? AND revoked_at IS NULL", hash).Take(&t).Error
	if err != nil {
		return nil, notFound(err)
	}
	return &t, nil
}

func (r *deviceTokenRepo) Bind(ctx context.Context, tokenID, deviceID uint64) (bool, error) {
	res := r.db.WithContext(ctx).Model(&model.DeviceToken{}).
		Where("id = ? AND (device_id IS NULL OR device_id = ?)", tokenID, deviceID).
		Update("device_id", deviceID)
	if res.Error != nil {
		return false, res.Error
	}
	if res.RowsAffected > 0 {
		return true, nil
	}
	// 值未变化时 MySQL 返回影响 0 行，需再确认。
	var n int64
	err := r.db.WithContext(ctx).Model(&model.DeviceToken{}).
		Where("id = ? AND device_id = ?", tokenID, deviceID).Count(&n).Error
	return n == 1, err
}

func (r *deviceTokenRepo) MarkUsed(ctx context.Context, id uint64, at time.Time) error {
	return r.db.WithContext(ctx).Model(&model.DeviceToken{}).Where("id = ?", id).Update("last_used_at", at).Error
}

// DeleteRevoked 删除已吊销的令牌；未吊销或不存在返回 false。
func (r *deviceTokenRepo) DeleteRevoked(ctx context.Context, id uint64) (bool, error) {
	res := r.db.WithContext(ctx).Where("id = ? AND revoked_at IS NOT NULL", id).Delete(&model.DeviceToken{})
	return res.RowsAffected > 0, res.Error
}

func (r *deviceTokenRepo) Revoke(ctx context.Context, id uint64, at time.Time) error {
	res := r.db.WithContext(ctx).Model(&model.DeviceToken{}).
		Where("id = ? AND revoked_at IS NULL", id).Update("revoked_at", at)
	if res.Error == nil && res.RowsAffected == 0 {
		return ErrNotFound
	}
	return res.Error
}

func (r *deviceTokenRepo) RevokeByDevice(ctx context.Context, deviceID uint64, at time.Time) (int64, error) {
	res := r.db.WithContext(ctx).Model(&model.DeviceToken{}).
		Where("device_id = ? AND revoked_at IS NULL", deviceID).Update("revoked_at", at)
	return res.RowsAffected, res.Error
}

func (r *deviceTokenRepo) List(ctx context.Context) ([]model.DeviceToken, error) {
	var out []model.DeviceToken
	err := r.db.WithContext(ctx).Order("id DESC").Limit(500).Find(&out).Error
	return out, err
}

// ---- 事件 ----

type eventRepo struct{ db *gorm.DB }

func (r *eventRepo) InsertIgnore(ctx context.Context, e *model.Event) (bool, error) {
	res := r.db.WithContext(ctx).Clauses(clause.Insert{Modifier: "IGNORE"}).Create(e)
	return res.RowsAffected == 1, res.Error
}

func (r *eventRepo) List(ctx context.Context, f EventFilter) ([]model.Event, int64, error) {
	q := r.db.WithContext(ctx).Model(&model.Event{})
	if f.DeviceID != 0 {
		q = q.Where("device_id = ?", f.DeviceID)
	}
	if f.Type != "" {
		q = q.Where("type = ?", f.Type)
	}
	if len(f.Levels) > 0 {
		q = q.Where("level IN ?", f.Levels)
	}
	if !f.Since.IsZero() {
		q = q.Where("created_at >= ?", f.Since)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var out []model.Event
	err := q.Order("id DESC").Offset(f.offset()).Limit(f.Size()).Find(&out).Error
	return out, total, err
}

func (r *eventRepo) CountSince(ctx context.Context, since time.Time) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.Event{}).Where("created_at >= ?", since).Count(&n).Error
	return n, err
}

func (r *eventRepo) CountBetween(ctx context.Context, from, to time.Time) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.Event{}).
		Where("created_at >= ? AND created_at < ?", from, to).Count(&n).Error
	return n, err
}

func (r *eventRepo) TypeCounts(ctx context.Context, since time.Time, limit int) ([]TypeCount, error) {
	var out []TypeCount
	err := r.db.WithContext(ctx).Model(&model.Event{}).Select("type, COUNT(*) AS count").
		Where("created_at >= ?", since).Group("type").Order("count DESC").Limit(limit).Scan(&out).Error
	return out, err
}

func (r *eventRepo) HourlyCounts(ctx context.Context, since time.Time) (map[string]int64, error) {
	type row struct {
		Hour string
		N    int64
	}
	var rows []row
	err := r.db.WithContext(ctx).Model(&model.Event{}).
		Select("DATE_FORMAT(created_at, '%Y-%m-%dT%H:00:00Z') AS hour, COUNT(*) AS n").
		Where("created_at >= ?", since).Group("hour").Scan(&rows).Error
	out := make(map[string]int64, len(rows))
	for _, r := range rows {
		out[r.Hour] = r.N
	}
	return out, err
}

func (r *eventRepo) DeviceBuckets(ctx context.Context, deviceID uint64, since time.Time, bucket string, tzOffset int) ([]ActivityBucket, error) {
	var rows []ActivityBucket
	err := r.db.WithContext(ctx).Model(&model.Event{}).
		Select("DATE_FORMAT(created_at + INTERVAL ? MINUTE, ?) AS bucket, COUNT(*) AS total, SUM(level IN ('warning','error')) AS bad", tzOffset, bucket).
		Where("device_id = ? AND created_at >= ?", deviceID, since).Group("bucket").Scan(&rows).Error
	return rows, err
}

func (r *eventRepo) DeleteByDevice(ctx context.Context, deviceID uint64) (int64, error) {
	res := r.db.WithContext(ctx).Where("device_id = ?", deviceID).Limit(10000).Delete(&model.Event{})
	return res.RowsAffected, res.Error
}

func (r *eventRepo) DeleteBefore(ctx context.Context, t time.Time) (int64, error) {
	res := r.db.WithContext(ctx).Where("created_at < ?", t).Limit(10000).Delete(&model.Event{})
	return res.RowsAffected, res.Error
}

// ---- 指令 ----

type invocationRepo struct{ db *gorm.DB }

func (r *invocationRepo) Create(ctx context.Context, inv *model.Invocation) error {
	return r.db.WithContext(ctx).Create(inv).Error
}

func (r *invocationRepo) Get(ctx context.Context, id uint64) (*model.Invocation, error) {
	var inv model.Invocation
	if err := r.db.WithContext(ctx).Take(&inv, id).Error; err != nil {
		return nil, notFound(err)
	}
	return &inv, nil
}

func (r *invocationRepo) GetByIdem(ctx context.Context, apiKeyID uint64, key string) (*model.Invocation, error) {
	var inv model.Invocation
	if err := r.db.WithContext(ctx).Where("api_key_id = ? AND idempotency_key = ?", apiKeyID, key).Take(&inv).Error; err != nil {
		return nil, notFound(err)
	}
	return &inv, nil
}

func (r *invocationRepo) GetByUID(ctx context.Context, uid string) (*model.Invocation, error) {
	var inv model.Invocation
	if err := r.db.WithContext(ctx).Where("uid = ?", uid).Take(&inv).Error; err != nil {
		return nil, notFound(err)
	}
	return &inv, nil
}

func (r *invocationRepo) Transition(ctx context.Context, id uint64, from []string, updates map[string]any) (bool, error) {
	res := r.db.WithContext(ctx).Model(&model.Invocation{}).
		Where("id = ? AND status IN ?", id, from).Updates(updates)
	return res.RowsAffected == 1, res.Error
}

func (r *invocationRepo) PendingForDevice(ctx context.Context, deviceID uint64, now time.Time) ([]model.Invocation, error) {
	var out []model.Invocation
	err := r.db.WithContext(ctx).
		Where("device_id = ? AND status = ? AND expires_at > ?", deviceID, model.InvPending, now).
		Order("id ASC").Limit(100).Find(&out).Error
	return out, err
}

func (r *invocationRepo) SentOverdue(ctx context.Context, now time.Time) ([]model.Invocation, error) {
	var out []model.Invocation
	err := r.db.WithContext(ctx).Where("status = ? AND expires_at <= ?", model.InvSent, now).Limit(500).Find(&out).Error
	return out, err
}

func (r *invocationRepo) SentForDevice(ctx context.Context, deviceID uint64) ([]model.Invocation, error) {
	var out []model.Invocation
	err := r.db.WithContext(ctx).Where("device_id = ? AND status = ?", deviceID, model.InvSent).Find(&out).Error
	return out, err
}

func (r *invocationRepo) List(ctx context.Context, f InvocationFilter) ([]model.Invocation, int64, error) {
	tx := r.db.WithContext(ctx).Model(&model.Invocation{})
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

func (r *invocationRepo) DeviceBuckets(ctx context.Context, deviceID uint64, since time.Time, bucket string, tzOffset int) ([]ActivityBucket, error) {
	var rows []ActivityBucket
	err := r.db.WithContext(ctx).Model(&model.Invocation{}).
		Select("DATE_FORMAT(created_at + INTERVAL ? MINUTE, ?) AS bucket, COUNT(*) AS total, SUM(status IN ('failed','lost','expired')) AS bad", tzOffset, bucket).
		Where("device_id = ? AND created_at >= ?", deviceID, since).Group("bucket").Scan(&rows).Error
	return rows, err
}

func (r *invocationRepo) ExpirePending(ctx context.Context, now time.Time) (int64, error) {
	res := r.db.WithContext(ctx).Model(&model.Invocation{}).
		Where("status = ? AND expires_at <= ?", model.InvPending, now).
		Updates(map[string]any{"status": model.InvExpired, "finished_at": now, "updated_at": now})
	return res.RowsAffected, res.Error
}

func (r *invocationRepo) StatusCounts(ctx context.Context, since time.Time) (map[string]int64, error) {
	type row struct {
		Status string
		N      int64
	}
	var rows []row
	err := r.db.WithContext(ctx).Model(&model.Invocation{}).Select("status, COUNT(*) AS n").
		Where("created_at >= ?", since).Group("status").Scan(&rows).Error
	out := make(map[string]int64, len(rows))
	for _, x := range rows {
		out[x.Status] = x.N
	}
	return out, err
}

func (r *invocationRepo) HourlyCounts(ctx context.Context, since time.Time) (map[string]int64, error) {
	type row struct {
		Hour string
		N    int64
	}
	var rows []row
	err := r.db.WithContext(ctx).Model(&model.Invocation{}).
		Select("DATE_FORMAT(created_at, '%Y-%m-%dT%H:00:00Z') AS hour, COUNT(*) AS n").
		Where("created_at >= ?", since).Group("hour").Scan(&rows).Error
	out := make(map[string]int64, len(rows))
	for _, x := range rows {
		out[x.Hour] = x.N
	}
	return out, err
}

func (r *invocationRepo) MarkAllSentLost(ctx context.Context, now time.Time) (int64, error) {
	res := r.db.WithContext(ctx).Model(&model.Invocation{}).Where("status = ?", model.InvSent).
		Updates(map[string]any{"status": model.InvLost, "finished_at": now, "updated_at": now,
			"error_message": "服务端重启，结果未知"})
	return res.RowsAffected, res.Error
}

func (r *invocationRepo) DeleteByDevice(ctx context.Context, deviceID uint64) (int64, error) {
	res := r.db.WithContext(ctx).Where("device_id = ?", deviceID).Limit(10000).Delete(&model.Invocation{})
	return res.RowsAffected, res.Error
}

func (r *invocationRepo) DeleteBefore(ctx context.Context, t time.Time) (int64, error) {
	res := r.db.WithContext(ctx).Where("created_at < ?", t).Limit(10000).Delete(&model.Invocation{})
	return res.RowsAffected, res.Error
}

// ---- 管理员 ----

type adminRepo struct{ db *gorm.DB }

func (r *adminRepo) Create(ctx context.Context, a *model.Admin) error {
	return r.db.WithContext(ctx).Create(a).Error
}

func (r *adminRepo) Get(ctx context.Context, id uint64) (*model.Admin, error) {
	var a model.Admin
	if err := r.db.WithContext(ctx).Take(&a, id).Error; err != nil {
		return nil, notFound(err)
	}
	return &a, nil
}

func (r *adminRepo) GetByUsername(ctx context.Context, username string) (*model.Admin, error) {
	var a model.Admin
	if err := r.db.WithContext(ctx).Where("username = ?", username).Take(&a).Error; err != nil {
		return nil, notFound(err)
	}
	return &a, nil
}

func (r *adminRepo) Update(ctx context.Context, id uint64, updates map[string]any) error {
	return r.db.WithContext(ctx).Model(&model.Admin{}).Where("id = ?", id).Updates(updates).Error
}

func (r *adminRepo) Count(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.Admin{}).Count(&n).Error
	return n, err
}

// ---- 刷新令牌 ----

type refreshTokenRepo struct{ db *gorm.DB }

func (r *refreshTokenRepo) Create(ctx context.Context, t *model.RefreshToken) error {
	return r.db.WithContext(ctx).Create(t).Error
}

func (r *refreshTokenRepo) FindActive(ctx context.Context, hash string, now time.Time) (*model.RefreshToken, error) {
	var t model.RefreshToken
	err := r.db.WithContext(ctx).
		Where("token_hash = ? AND revoked_at IS NULL AND expires_at > ?", hash, now).Take(&t).Error
	if err != nil {
		return nil, notFound(err)
	}
	return &t, nil
}

func (r *refreshTokenRepo) Revoke(ctx context.Context, id uint64, at time.Time) error {
	return r.db.WithContext(ctx).Model(&model.RefreshToken{}).
		Where("id = ? AND revoked_at IS NULL", id).Update("revoked_at", at).Error
}

func (r *refreshTokenRepo) RevokeAllForAdmin(ctx context.Context, adminID uint64, at time.Time) error {
	return r.db.WithContext(ctx).Model(&model.RefreshToken{}).
		Where("admin_id = ? AND revoked_at IS NULL", adminID).Update("revoked_at", at).Error
}

func (r *refreshTokenRepo) DeleteExpired(ctx context.Context, now time.Time) (int64, error) {
	res := r.db.WithContext(ctx).Where("expires_at < ?", now.Add(-24*time.Hour)).Delete(&model.RefreshToken{})
	return res.RowsAffected, res.Error
}

// ---- 审计 ----

type auditRepo struct{ db *gorm.DB }

func (r *auditRepo) Insert(ctx context.Context, l *model.AuditLog) error {
	return r.db.WithContext(ctx).Create(l).Error
}

func (r *auditRepo) DeleteBefore(ctx context.Context, t time.Time) (int64, error) {
	res := r.db.WithContext(ctx).Where("created_at < ?", t).Limit(10000).Delete(&model.AuditLog{})
	return res.RowsAffected, res.Error
}

func (r *auditRepo) List(ctx context.Context, q ListQuery) ([]model.AuditLog, int64, error) {
	tx := r.db.WithContext(ctx).Model(&model.AuditLog{})
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var out []model.AuditLog
	err := tx.Order("id DESC").Offset(q.offset()).Limit(q.Size()).Find(&out).Error
	return out, total, err
}
