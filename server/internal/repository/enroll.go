package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/icloudza/ResidentKit/server/internal/model"
)

type EnrollmentRepository interface {
	Create(ctx context.Context, e *model.Enrollment) error
	Get(ctx context.Context, id uint64) (*model.Enrollment, error)
	// Latest 设备最近一次申请。
	Latest(ctx context.Context, deviceUID string) (*model.Enrollment, error)
	Pending(ctx context.Context) ([]model.Enrollment, error)
	CountPending(ctx context.Context) (int64, error)
	// Transition 仅当状态仍在 from 中时才更新。
	Transition(ctx context.Context, id uint64, from []string, updates map[string]any) (bool, error)
	ExpireBefore(ctx context.Context, t time.Time) (int64, error)
	// SwapToken 仅当已完成且 token_id 仍为 oldToken 时换成 newToken。
	SwapToken(ctx context.Context, id, oldToken, newToken uint64) (bool, error)
}

type enrollmentRepo struct{ db *gorm.DB }

func (r *enrollmentRepo) Create(ctx context.Context, e *model.Enrollment) error {
	return r.db.WithContext(ctx).Create(e).Error
}

func (r *enrollmentRepo) Get(ctx context.Context, id uint64) (*model.Enrollment, error) {
	var e model.Enrollment
	return &e, notFound(r.db.WithContext(ctx).First(&e, id).Error)
}

func (r *enrollmentRepo) Latest(ctx context.Context, deviceUID string) (*model.Enrollment, error) {
	var e model.Enrollment
	return &e, notFound(r.db.WithContext(ctx).Where("device_uid = ?", deviceUID).Order("id DESC").First(&e).Error)
}

func (r *enrollmentRepo) Pending(ctx context.Context) ([]model.Enrollment, error) {
	var list []model.Enrollment
	return list, r.db.WithContext(ctx).Where("status = ?", model.EnrollPending).Order("id").Find(&list).Error
}

func (r *enrollmentRepo) CountPending(ctx context.Context) (int64, error) {
	var n int64
	return n, r.db.WithContext(ctx).Model(&model.Enrollment{}).Where("status = ?", model.EnrollPending).Count(&n).Error
}

func (r *enrollmentRepo) Transition(ctx context.Context, id uint64, from []string, updates map[string]any) (bool, error) {
	updates["updated_at"] = time.Now().UTC()
	res := r.db.WithContext(ctx).Model(&model.Enrollment{}).Where("id = ? AND status IN ?", id, from).Updates(updates)
	return res.RowsAffected > 0, res.Error
}

func (r *enrollmentRepo) ExpireBefore(ctx context.Context, t time.Time) (int64, error) {
	res := r.db.WithContext(ctx).Model(&model.Enrollment{}).
		Where("status = ? AND created_at < ?", model.EnrollPending, t).
		Updates(map[string]any{"status": model.EnrollExpired, "updated_at": time.Now().UTC()})
	return res.RowsAffected, res.Error
}

func (r *enrollmentRepo) SwapToken(ctx context.Context, id, oldToken, newToken uint64) (bool, error) {
	res := r.db.WithContext(ctx).Model(&model.Enrollment{}).
		Where("id = ? AND status = ? AND token_id = ?", id, model.EnrollCompleted, oldToken).
		Updates(map[string]any{"token_id": newToken, "updated_at": time.Now().UTC()})
	return res.RowsAffected > 0, res.Error
}

// EnrollTicketRepository 接入码。
type EnrollTicketRepository interface {
	Create(ctx context.Context, t *model.EnrollTicket) error
	Get(ctx context.Context, id uint64) (*model.EnrollTicket, error)
	FindByHash(ctx context.Context, hash string) (*model.EnrollTicket, error)
	List(ctx context.Context) ([]model.EnrollTicket, error)
	Revoke(ctx context.Context, id uint64, at time.Time) error
	// TakeUse 占用一次：未吊销、未过期且未用完时计数加一并返回 true。
	TakeUse(ctx context.Context, id uint64, now time.Time) (bool, error)
}

type enrollTicketRepo struct{ db *gorm.DB }

func (r *enrollTicketRepo) Create(ctx context.Context, t *model.EnrollTicket) error {
	return r.db.WithContext(ctx).Create(t).Error
}

func (r *enrollTicketRepo) Get(ctx context.Context, id uint64) (*model.EnrollTicket, error) {
	var t model.EnrollTicket
	return &t, notFound(r.db.WithContext(ctx).First(&t, id).Error)
}

func (r *enrollTicketRepo) FindByHash(ctx context.Context, hash string) (*model.EnrollTicket, error) {
	var t model.EnrollTicket
	return &t, notFound(r.db.WithContext(ctx).Where("token_hash = ?", hash).Take(&t).Error)
}

func (r *enrollTicketRepo) List(ctx context.Context) ([]model.EnrollTicket, error) {
	var list []model.EnrollTicket
	return list, r.db.WithContext(ctx).Order("id DESC").Limit(200).Find(&list).Error
}

func (r *enrollTicketRepo) Revoke(ctx context.Context, id uint64, at time.Time) error {
	res := r.db.WithContext(ctx).Model(&model.EnrollTicket{}).Where("id = ? AND revoked_at IS NULL", id).Update("revoked_at", at)
	if res.Error == nil && res.RowsAffected == 0 {
		return ErrNotFound
	}
	return res.Error
}

func (r *enrollTicketRepo) TakeUse(ctx context.Context, id uint64, now time.Time) (bool, error) {
	res := r.db.WithContext(ctx).Model(&model.EnrollTicket{}).
		Where("id = ? AND revoked_at IS NULL AND expires_at > ? AND (max_uses = 0 OR uses < max_uses)", id, now).
		Updates(map[string]any{"uses": gorm.Expr("uses + 1"), "last_used_at": now})
	return res.RowsAffected > 0, res.Error
}
