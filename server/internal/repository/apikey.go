package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/icloudza/ResidentKit/server/internal/model"
)

type APIKeyRepository interface {
	Create(ctx context.Context, k *model.APIKey) error
	FindActiveByHash(ctx context.Context, hash string) (*model.APIKey, error)
	List(ctx context.Context) ([]model.APIKey, error)
	CountActive(ctx context.Context) (int64, error)
	Revoke(ctx context.Context, id uint64, at time.Time) error
	// DeleteRevoked 只删除已吊销的密钥，未吊销或不存在时返回 false。
	DeleteRevoked(ctx context.Context, id uint64) (bool, error)
	Touch(ctx context.Context, id uint64, at time.Time, ip string) error
	Get(ctx context.Context, id uint64) (*model.APIKey, error)
	Update(ctx context.Context, id uint64, updates map[string]any) error
}

func (r *apiKeyRepo) Get(ctx context.Context, id uint64) (*model.APIKey, error) {
	var k model.APIKey
	return &k, notFound(r.db.WithContext(ctx).First(&k, id).Error)
}

func (r *apiKeyRepo) Update(ctx context.Context, id uint64, updates map[string]any) error {
	return r.db.WithContext(ctx).Model(&model.APIKey{}).Where("id = ? AND revoked_at IS NULL", id).Updates(updates).Error
}

type apiKeyRepo struct{ db *gorm.DB }

func (r *apiKeyRepo) Create(ctx context.Context, k *model.APIKey) error {
	return r.db.WithContext(ctx).Create(k).Error
}

func (r *apiKeyRepo) FindActiveByHash(ctx context.Context, hash string) (*model.APIKey, error) {
	var k model.APIKey
	return &k, notFound(r.db.WithContext(ctx).Where("key_hash = ? AND revoked_at IS NULL", hash).Take(&k).Error)
}

func (r *apiKeyRepo) List(ctx context.Context) ([]model.APIKey, error) {
	var list []model.APIKey
	return list, r.db.WithContext(ctx).Order("id DESC").Limit(500).Find(&list).Error
}

func (r *apiKeyRepo) CountActive(ctx context.Context) (int64, error) {
	var n int64
	return n, r.db.WithContext(ctx).Model(&model.APIKey{}).Where("revoked_at IS NULL").Count(&n).Error
}

func (r *apiKeyRepo) Revoke(ctx context.Context, id uint64, at time.Time) error {
	res := r.db.WithContext(ctx).Model(&model.APIKey{}).Where("id = ? AND revoked_at IS NULL", id).Update("revoked_at", at)
	if res.Error == nil && res.RowsAffected == 0 {
		return ErrNotFound
	}
	return res.Error
}

func (r *apiKeyRepo) DeleteRevoked(ctx context.Context, id uint64) (bool, error) {
	res := r.db.WithContext(ctx).Where("id = ? AND revoked_at IS NOT NULL", id).Delete(&model.APIKey{})
	return res.RowsAffected > 0, res.Error
}

func (r *apiKeyRepo) Touch(ctx context.Context, id uint64, at time.Time, ip string) error {
	return r.db.WithContext(ctx).Model(&model.APIKey{}).Where("id = ?", id).
		Updates(map[string]any{"last_used_at": at, "last_used_ip": ip}).Error
}
