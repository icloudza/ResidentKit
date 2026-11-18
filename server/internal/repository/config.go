package repository

import (
	"context"

	"gorm.io/gorm"

	"github.com/icloudza/ResidentKit/server/internal/model"
)

type deviceConfigRepo struct{ db *gorm.DB }

func (r *deviceConfigRepo) Get(ctx context.Context, deviceID uint64) (*model.DeviceConfig, error) {
	var c model.DeviceConfig
	return &c, notFound(r.db.WithContext(ctx).First(&c, "device_id = ?", deviceID).Error)
}

// Save 新建或整体替换。
func (r *deviceConfigRepo) Save(ctx context.Context, c *model.DeviceConfig) error {
	return r.db.WithContext(ctx).Save(c).Error
}

// SetError 只在版本仍是 version 时记录（避免旧版本的结果覆盖新配置）。
func (r *deviceConfigRepo) SetError(ctx context.Context, deviceID uint64, version int64, msg string) error {
	return r.db.WithContext(ctx).Model(&model.DeviceConfig{}).
		Where("device_id = ? AND version = ?", deviceID, version).Update("error", msg).Error
}

func (r *deviceConfigRepo) Delete(ctx context.Context, deviceID uint64) error {
	return r.db.WithContext(ctx).Where("device_id = ?", deviceID).Delete(&model.DeviceConfig{}).Error
}
