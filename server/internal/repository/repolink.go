package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/icloudza/ResidentKit/server/internal/model"
)

// RepoLinkRepository 软件源安装链接与下载记录。
type RepoLinkRepository interface {
	Create(ctx context.Context, l *model.RepoLink) error
	Get(ctx context.Context, id uint64) (*model.RepoLink, error)
	FindByHash(ctx context.Context, hash string) (*model.RepoLink, error)
	List(ctx context.Context) ([]model.RepoLink, error)
	Revoke(ctx context.Context, id uint64, at time.Time) error
	// Update 修改未吊销的链接（名称、有效期、下载次数上限）。
	Update(ctx context.Context, id uint64, updates map[string]any) error
	// DeleteRevoked 删除已吊销的链接及其下载记录；未吊销或不存在时返回 false。
	DeleteRevoked(ctx context.Context, id uint64) (bool, error)
	// TakeDownload 占用一次下载名额：链接未吊销、未过期且未用完时计数加一并返回 true。
	TakeDownload(ctx context.Context, id uint64, now time.Time) (bool, error)
	AddDownload(ctx context.Context, d *model.RepoDownload) error
	// Downloads 某链接最近的下载记录，新到旧。
	Downloads(ctx context.Context, linkID uint64, limit int) ([]model.RepoDownload, error)
}

type repoLinkRepo struct{ db *gorm.DB }

func (r *repoLinkRepo) Create(ctx context.Context, l *model.RepoLink) error {
	return r.db.WithContext(ctx).Create(l).Error
}

func (r *repoLinkRepo) Get(ctx context.Context, id uint64) (*model.RepoLink, error) {
	var l model.RepoLink
	return &l, notFound(r.db.WithContext(ctx).First(&l, id).Error)
}

func (r *repoLinkRepo) FindByHash(ctx context.Context, hash string) (*model.RepoLink, error) {
	var l model.RepoLink
	return &l, notFound(r.db.WithContext(ctx).Where("token_hash = ?", hash).Take(&l).Error)
}

func (r *repoLinkRepo) List(ctx context.Context) ([]model.RepoLink, error) {
	var list []model.RepoLink
	return list, r.db.WithContext(ctx).Order("id DESC").Limit(500).Find(&list).Error
}

func (r *repoLinkRepo) Revoke(ctx context.Context, id uint64, at time.Time) error {
	res := r.db.WithContext(ctx).Model(&model.RepoLink{}).Where("id = ? AND revoked_at IS NULL", id).Update("revoked_at", at)
	if res.Error == nil && res.RowsAffected == 0 {
		return ErrNotFound
	}
	return res.Error
}

func (r *repoLinkRepo) Update(ctx context.Context, id uint64, updates map[string]any) error {
	res := r.db.WithContext(ctx).Model(&model.RepoLink{}).Where("id = ? AND revoked_at IS NULL", id).Updates(updates)
	if res.Error == nil && res.RowsAffected == 0 {
		return ErrNotFound
	}
	return res.Error
}

func (r *repoLinkRepo) DeleteRevoked(ctx context.Context, id uint64) (bool, error) {
	var ok bool
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Where("id = ? AND revoked_at IS NOT NULL", id).Delete(&model.RepoLink{})
		if res.Error != nil || res.RowsAffected == 0 {
			return res.Error
		}
		ok = true
		return tx.Where("link_id = ?", id).Delete(&model.RepoDownload{}).Error
	})
	return ok, err
}

func (r *repoLinkRepo) TakeDownload(ctx context.Context, id uint64, now time.Time) (bool, error) {
	res := r.db.WithContext(ctx).Model(&model.RepoLink{}).
		Where("id = ? AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > ?) AND (max_downloads = 0 OR downloads < max_downloads)", id, now).
		Updates(map[string]any{"downloads": gorm.Expr("downloads + 1"), "last_used_at": now})
	return res.RowsAffected > 0, res.Error
}

func (r *repoLinkRepo) AddDownload(ctx context.Context, d *model.RepoDownload) error {
	return r.db.WithContext(ctx).Create(d).Error
}

func (r *repoLinkRepo) Downloads(ctx context.Context, linkID uint64, limit int) ([]model.RepoDownload, error) {
	var list []model.RepoDownload
	return list, r.db.WithContext(ctx).Where("link_id = ?", linkID).Order("created_at DESC").Limit(limit).Find(&list).Error
}
