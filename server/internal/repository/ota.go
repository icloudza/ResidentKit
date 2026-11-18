package repository

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/icloudza/ResidentKit/server/internal/model"
)

type ReleaseRepository interface {
	Create(ctx context.Context, r *model.Release) error
	Get(ctx context.Context, id uint64) (*model.Release, error)
	GetByVersion(ctx context.Context, version string) (*model.Release, error)
	// All 全部发布，新到旧。
	All(ctx context.Context) ([]model.Release, error)
	SetStatus(ctx context.Context, id uint64, status string) error
	// LatestInRepo 已同步到软件源且未撤回的最新版本（版本号随上传递增，按 id 取最新即最高版本）。
	LatestInRepo(ctx context.Context) (*model.Release, error)
	SetInRepo(ctx context.Context, id uint64, inRepo bool) error
}

type RolloutRepository interface {
	Create(ctx context.Context, r *model.Rollout) error
	Get(ctx context.Context, id uint64) (*model.Rollout, error)
	ListByRelease(ctx context.Context, releaseID uint64) ([]model.Rollout, error)
	Active(ctx context.Context) ([]model.Rollout, error)
	// Transition 仅当状态仍在 from 中时才更新。
	Transition(ctx context.Context, id uint64, from []string, to string) (bool, error)
}

// UpdateCount 某次发布各状态的设备数。
type UpdateCount struct {
	ReleaseID uint64
	Status    string
	Count     int64
}

type DeviceUpdateRepository interface {
	// CreateIgnore 写入一条，(设备, 版本) 已存在时忽略并返回 false。
	CreateIgnore(ctx context.Context, u *model.DeviceUpdate) (bool, error)
	Get(ctx context.Context, id uint64) (*model.DeviceUpdate, error)
	Find(ctx context.Context, deviceID, releaseID uint64) (*model.DeviceUpdate, error)
	ListByRelease(ctx context.Context, releaseID uint64) ([]model.DeviceUpdate, error)
	ListByReleases(ctx context.Context, releaseIDs []uint64) ([]model.DeviceUpdate, error)
	ListByDevice(ctx context.Context, deviceID uint64, limit int) ([]model.DeviceUpdate, error)
	Unfinished(ctx context.Context) ([]model.DeviceUpdate, error)
	Counts(ctx context.Context, releaseIDs []uint64) ([]UpdateCount, error)
	// Transition 仅当状态仍在 from 中时才更新（from 为空表示不限）。
	Transition(ctx context.Context, id uint64, from []string, updates map[string]any) (bool, error)
	Delete(ctx context.Context, id uint64) error
	DeleteByDevice(ctx context.Context, deviceID uint64) error
}

type releaseRepo struct{ db *gorm.DB }

func (r *releaseRepo) Create(ctx context.Context, m *model.Release) error {
	return r.db.WithContext(ctx).Create(m).Error
}

func (r *releaseRepo) Get(ctx context.Context, id uint64) (*model.Release, error) {
	var m model.Release
	return &m, notFound(r.db.WithContext(ctx).First(&m, id).Error)
}

func (r *releaseRepo) GetByVersion(ctx context.Context, version string) (*model.Release, error) {
	var m model.Release
	return &m, notFound(r.db.WithContext(ctx).Where("version = ?", version).First(&m).Error)
}

func (r *releaseRepo) All(ctx context.Context) ([]model.Release, error) {
	var list []model.Release
	// 版本号随 id 严格递增，最新的在前；上限只是保护，正常远达不到。
	return list, r.db.WithContext(ctx).Order("id DESC").Limit(500).Find(&list).Error
}

func (r *releaseRepo) LatestInRepo(ctx context.Context) (*model.Release, error) {
	var m model.Release
	return &m, notFound(r.db.WithContext(ctx).Where("in_repo = ? AND status = ?", true, model.ReleasePublished).
		Order("id DESC").Take(&m).Error)
}

func (r *releaseRepo) SetInRepo(ctx context.Context, id uint64, inRepo bool) error {
	return r.db.WithContext(ctx).Model(&model.Release{}).Where("id = ?", id).
		Updates(map[string]any{"in_repo": inRepo, "updated_at": time.Now().UTC()}).Error
}

func (r *releaseRepo) SetStatus(ctx context.Context, id uint64, status string) error {
	return r.db.WithContext(ctx).Model(&model.Release{}).Where("id = ?", id).
		Updates(map[string]any{"status": status, "updated_at": time.Now().UTC()}).Error
}

type rolloutRepo struct{ db *gorm.DB }

func (r *rolloutRepo) Create(ctx context.Context, m *model.Rollout) error {
	return r.db.WithContext(ctx).Create(m).Error
}

func (r *rolloutRepo) Get(ctx context.Context, id uint64) (*model.Rollout, error) {
	var m model.Rollout
	return &m, notFound(r.db.WithContext(ctx).First(&m, id).Error)
}

func (r *rolloutRepo) ListByRelease(ctx context.Context, releaseID uint64) ([]model.Rollout, error) {
	var list []model.Rollout
	return list, r.db.WithContext(ctx).Where("release_id = ?", releaseID).Order("id DESC").Find(&list).Error
}

func (r *rolloutRepo) Active(ctx context.Context) ([]model.Rollout, error) {
	var list []model.Rollout
	return list, r.db.WithContext(ctx).Where("status = ?", model.RolloutActive).Order("id").Find(&list).Error
}

func (r *rolloutRepo) Transition(ctx context.Context, id uint64, from []string, to string) (bool, error) {
	res := r.db.WithContext(ctx).Model(&model.Rollout{}).Where("id = ? AND status IN ?", id, from).
		Updates(map[string]any{"status": to, "updated_at": time.Now().UTC()})
	return res.RowsAffected > 0, res.Error
}

type deviceUpdateRepo struct{ db *gorm.DB }

func (r *deviceUpdateRepo) CreateIgnore(ctx context.Context, u *model.DeviceUpdate) (bool, error) {
	res := r.db.WithContext(ctx).Clauses(clause.Insert{Modifier: "IGNORE"}).Create(u)
	return res.RowsAffected > 0, res.Error
}

func (r *deviceUpdateRepo) Get(ctx context.Context, id uint64) (*model.DeviceUpdate, error) {
	var m model.DeviceUpdate
	return &m, notFound(r.db.WithContext(ctx).First(&m, id).Error)
}

func (r *deviceUpdateRepo) Find(ctx context.Context, deviceID, releaseID uint64) (*model.DeviceUpdate, error) {
	var m model.DeviceUpdate
	return &m, notFound(r.db.WithContext(ctx).Where("device_id = ? AND release_id = ?", deviceID, releaseID).First(&m).Error)
}

func (r *deviceUpdateRepo) ListByRelease(ctx context.Context, releaseID uint64) ([]model.DeviceUpdate, error) {
	var list []model.DeviceUpdate
	return list, r.db.WithContext(ctx).Where("release_id = ?", releaseID).Order("id").Find(&list).Error
}

func (r *deviceUpdateRepo) ListByReleases(ctx context.Context, ids []uint64) ([]model.DeviceUpdate, error) {
	var list []model.DeviceUpdate
	if len(ids) == 0 {
		return list, nil
	}
	return list, r.db.WithContext(ctx).Where("release_id IN ?", ids).Find(&list).Error
}

func (r *deviceUpdateRepo) ListByDevice(ctx context.Context, deviceID uint64, limit int) ([]model.DeviceUpdate, error) {
	var list []model.DeviceUpdate
	return list, r.db.WithContext(ctx).Where("device_id = ?", deviceID).Order("id DESC").Limit(limit).Find(&list).Error
}

func (r *deviceUpdateRepo) Unfinished(ctx context.Context) ([]model.DeviceUpdate, error) {
	var list []model.DeviceUpdate
	return list, r.db.WithContext(ctx).Where("status IN ?", []string{model.UpdQueued, model.UpdDownloading}).Find(&list).Error
}

func (r *deviceUpdateRepo) Counts(ctx context.Context, releaseIDs []uint64) ([]UpdateCount, error) {
	var out []UpdateCount
	if len(releaseIDs) == 0 {
		return out, nil
	}
	return out, r.db.WithContext(ctx).Model(&model.DeviceUpdate{}).Where("release_id IN ?", releaseIDs).
		Select("release_id, status, COUNT(*) AS count").Group("release_id, status").Scan(&out).Error
}

func (r *deviceUpdateRepo) Transition(ctx context.Context, id uint64, from []string, updates map[string]any) (bool, error) {
	q := r.db.WithContext(ctx).Model(&model.DeviceUpdate{}).Where("id = ?", id)
	if len(from) > 0 {
		q = q.Where("status IN ?", from)
	}
	updates["updated_at"] = time.Now().UTC()
	res := q.Updates(updates)
	return res.RowsAffected > 0, res.Error
}

func (r *deviceUpdateRepo) Delete(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Delete(&model.DeviceUpdate{}, id).Error
}

func (r *deviceUpdateRepo) DeleteByDevice(ctx context.Context, deviceID uint64) error {
	return r.db.WithContext(ctx).Where("device_id = ?", deviceID).Delete(&model.DeviceUpdate{}).Error
}
