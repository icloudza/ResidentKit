package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"
	"unicode/utf16"

	"go.uber.org/zap"
	"gorm.io/datatypes"

	"github.com/icloudza/ResidentKit/server/internal/dto"
	"github.com/icloudza/ResidentKit/server/internal/model"
	"github.com/icloudza/ResidentKit/server/internal/pkg/errcode"
	"github.com/icloudza/ResidentKit/server/internal/repository"
)

var configActor = Actor{Username: "config"}

// ConfigService 远程配置（docs/config.md）。
//
// 设备在 hello 中上报可配置项（schema）与已生效的版本；服务端为每台设备保存一份期望配置，
// 版本号递增。保存时立即下发 system.config/apply，设备离线或版本不一致时在上线后补发；
// 设备生效后发 system.config.applied 事件，服务端据此记录已同步的版本。
type ConfigService struct {
	repo    *repository.Set
	devices *DeviceService
	inv     *InvocationService
	audit   *AuditService
	bus     *Bus
	log     *zap.Logger
}

func NewConfigService(r *repository.Set, devices *DeviceService, inv *InvocationService, audit *AuditService,
	bus *Bus, log *zap.Logger) *ConfigService {
	return &ConfigService{repo: r, devices: devices, inv: inv, audit: audit, bus: bus, log: log.Named("config")}
}

// normalizeSchema 只保留结构合法的配置项，避免把设备发来的任意 JSON 原样存库。
func normalizeSchema(raw json.RawMessage) datatypes.JSON {
	var s dto.ConfigSchema
	if len(raw) == 0 || len(raw) > 32<<10 || json.Unmarshal(raw, &s) != nil {
		return nil
	}
	out := dto.ConfigSchema{}
	for mod, items := range s {
		if len(mod) > 64 || len(items) > 50 {
			continue
		}
		var keep []dto.ConfigItem
		for _, it := range items {
			if it.Key == "" || len(it.Key) > 64 || it.Label == "" {
				continue
			}
			if _, err := normalizeValue(it, it.Default); err != nil {
				continue
			}
			keep = append(keep, it)
		}
		if len(keep) > 0 {
			out[mod] = keep
		}
	}
	if len(out) == 0 {
		return nil
	}
	b, _ := json.Marshal(out)
	return b
}

// normalizeValue 按配置项校验一个值（与设备端 RKNormalizeConfigValue 一致）。
func normalizeValue(it dto.ConfigItem, v any) (any, error) {
	switch it.Type {
	case "bool":
		if b, ok := v.(bool); ok {
			return b, nil
		}
		return nil, errors.New("应为开关")
	case "int", "number":
		f, ok := v.(float64)
		if !ok || math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, errors.New("应为数字")
		}
		if it.Type == "int" && f != math.Trunc(f) {
			return nil, errors.New("应为整数")
		}
		if (it.Min != nil && f < *it.Min) || (it.Max != nil && f > *it.Max) {
			return nil, fmt.Errorf("应在 %s–%s 之间", fmtBound(it.Min), fmtBound(it.Max))
		}
		return f, nil
	case "string":
		s, ok := v.(string)
		limit := it.MaxLen
		if limit <= 0 {
			limit = 256
		}
		if !ok || len(utf16.Encode([]rune(s))) > limit {
			return nil, fmt.Errorf("应为不超过 %d 字的文本", limit)
		}
		return s, nil
	case "enum":
		if s, ok := v.(string); ok && slices.Contains(it.Options, s) {
			return s, nil
		}
		return nil, errors.New("不是可选值之一")
	}
	return nil, errors.New("未知类型")
}

func fmtBound(p *float64) string {
	if p == nil {
		return ""
	}
	return fmt.Sprint(*p)
}

func schemaOf(d *model.Device) dto.ConfigSchema {
	s := dto.ConfigSchema{}
	if len(d.ConfigSchema) > 0 {
		_ = json.Unmarshal(d.ConfigSchema, &s)
	}
	return s
}

func supportsConfig(d *model.Device) bool {
	return slices.Contains(dto.DeviceFrom(d).RemoteCommands["system.config"], "apply")
}

// Get 设备的配置声明、期望配置与同步状态。
func (s *ConfigService) Get(ctx context.Context, deviceID uint64) (*dto.DeviceConfig, error) {
	d, err := s.devices.Get(ctx, deviceID)
	if err != nil {
		return nil, err
	}
	out := &dto.DeviceConfig{Schema: schemaOf(d), AppliedVersion: d.ConfigVersion, Supported: supportsConfig(d),
		Config: map[string]map[string]any{}}
	c, err := s.repo.DeviceConfigs.Get(ctx, deviceID)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}
	if err == nil {
		out.Version, out.Error, out.UpdatedAt = c.Version, c.Error, &c.UpdatedAt
		_ = json.Unmarshal(c.Config, &out.Config)
	}
	switch {
	case out.Version == 0 || out.Version == out.AppliedVersion:
		out.Status = "synced"
	case out.Error != "":
		out.Status = "rejected"
	default:
		out.Status = "pending"
	}
	return out, nil
}

// Update 保存期望配置（整体替换，未给出的项恢复默认）并立即下发。
func (s *ConfigService) Update(ctx context.Context, a Actor, deviceID uint64, cfg map[string]map[string]any) (*dto.DeviceConfig, error) {
	d, err := s.devices.Get(ctx, deviceID)
	if err != nil {
		return nil, err
	}
	if !supportsConfig(d) {
		return nil, errcode.Conflict.WithMessage("设备版本 %s 不支持远程配置", d.Version)
	}
	schema := schemaOf(d)
	clean := map[string]map[string]any{}
	for mod, kv := range cfg {
		items := map[string]dto.ConfigItem{}
		for _, it := range schema[mod] {
			items[it.Key] = it
		}
		if len(items) == 0 {
			return nil, errcode.InvalidParams.WithMessage("设备上没有模块 %s 的配置项", mod)
		}
		for k, v := range kv {
			it, ok := items[k]
			if !ok {
				return nil, errcode.InvalidParams.WithMessage("%s 没有配置项 %s", mod, k)
			}
			nv, err := normalizeValue(it, v)
			if err != nil {
				return nil, errcode.InvalidParams.WithMessage("%s：%s", it.Label, err.Error())
			}
			if def, _ := normalizeValue(it, it.Default); def == nv {
				continue // 与默认值相同的不保存
			}
			if clean[mod] == nil {
				clean[mod] = map[string]any{}
			}
			clean[mod][k] = nv
		}
	}
	var version int64 = 1
	if cur, err := s.repo.DeviceConfigs.Get(ctx, deviceID); err == nil {
		version = cur.Version + 1
	}
	version = max(version, d.ConfigVersion+1)
	body, _ := json.Marshal(clean)
	c := &model.DeviceConfig{DeviceID: deviceID, Version: version, Config: body, UpdatedAt: time.Now().UTC()}
	if a.AdminID != 0 {
		id := a.AdminID
		c.UpdatedBy = &id
	}
	if err := s.repo.DeviceConfigs.Save(ctx, c); err != nil {
		return nil, err
	}
	s.audit.Record(ctx, a, AuditDeviceConfig, fmt.Sprint(deviceID), true, map[string]any{"version": version, "config": clean})
	if d.Online {
		s.push(ctx, d, c)
	}
	s.publish(deviceID)
	return s.Get(ctx, deviceID)
}

// push 下发 system.config/apply；设备不在线时等上线后再发。
func (s *ConfigService) push(ctx context.Context, d *model.Device, c *model.DeviceConfig) {
	cfg := map[string]any{}
	_ = json.Unmarshal(c.Config, &cfg)
	_, err := s.inv.Create(ctx, configActor, d.ID, dto.InvokeRequest{
		Module: "system.config", Command: "apply", TTLSeconds: -1,
		Arguments: map[string]any{"version": c.Version, "config": cfg},
	})
	if err != nil && !errors.Is(err, errcode.DeviceOffline) {
		s.log.Warn("push config", zap.Uint64("device", d.ID), zap.Error(err))
	}
}

// OnDeviceOnline 设备上线：已生效版本与期望不一致时补发（被拒绝的版本除外）。
func (s *ConfigService) OnDeviceOnline(d *model.Device) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		c, err := s.repo.DeviceConfigs.Get(ctx, d.ID)
		// 已被设备拒绝的版本不再重发（否则每次重连都会再被拒一次），等管理员修改后再下发。
		if err != nil || c.Version == d.ConfigVersion || c.Error != "" || !supportsConfig(d) {
			return
		}
		s.push(ctx, d, c)
	}()
}

// OnEvent 处理 system.config.applied / rejected。
func (s *ConfigService) OnEvent(ctx context.Context, deviceID uint64, typ string, payload json.RawMessage) {
	var p struct {
		Version int64  `json:"version"`
		Error   string `json:"error"`
	}
	switch typ {
	case "system.config.applied":
		if json.Unmarshal(payload, &p) == nil && p.Version > 0 {
			_ = s.repo.Devices.SetConfigVersion(ctx, deviceID, p.Version)
			_ = s.repo.DeviceConfigs.SetError(ctx, deviceID, p.Version, "")
			s.publish(deviceID)
		}
	case "system.config.rejected":
		if json.Unmarshal(payload, &p) == nil && p.Version > 0 {
			_ = s.repo.DeviceConfigs.SetError(ctx, deviceID, p.Version, trunc(p.Error, 255))
			s.publish(deviceID)
		}
	}
}

func (s *ConfigService) publish(deviceID uint64) {
	s.bus.Publish(TopicConfigChanged, map[string]uint64{"device_id": deviceID})
}
