package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"gorm.io/datatypes"
	"strings"
	"time"

	"github.com/icloudza/ResidentKit/server/internal/dto"
	"github.com/icloudza/ResidentKit/server/internal/model"
	"github.com/icloudza/ResidentKit/server/internal/pkg/errcode"
	"github.com/icloudza/ResidentKit/server/internal/pkg/secure"
	"github.com/icloudza/ResidentKit/server/internal/repository"
)

const deviceTokenPrefix = "rkd_"

// HelloInfo 设备 hello 中的信息。
type HelloInfo struct {
	DeviceUID      string
	Name           string
	Version        string
	Network        string
	IP             string
	RemoteCommands map[string][]string
	ConfigVersion  int64
	ConfigSchema   json.RawMessage
	CommandSchema  json.RawMessage
	Info           json.RawMessage
}

// normalizeInfo 只保留已知字段并截断，避免把任意 JSON 存库；没有有效字段返回 nil。
func normalizeInfo(raw json.RawMessage) datatypes.JSON {
	var in dto.DeviceInfo
	if len(raw) == 0 || len(raw) > 4096 || json.Unmarshal(raw, &in) != nil {
		return nil
	}
	in.Model, in.OS, in.AppVersion = trunc(in.Model, 64), trunc(in.OS, 64), trunc(in.AppVersion, 32)
	if in.Model == "" && in.OS == "" && in.AppVersion == "" {
		return nil
	}
	b, _ := json.Marshal(in)
	return b
}

// ApplyInfoResult net.link/info 的结果：刷新设备信息与资源指标，并推送给控制台。
func (s *DeviceService) ApplyInfoResult(ctx context.Context, id uint64, payload json.RawMessage) {
	var r struct {
		Info    json.RawMessage `json:"info"`
		Metrics json.RawMessage `json:"metrics"`
	}
	if json.Unmarshal(payload, &r) != nil {
		return
	}
	if info := normalizeInfo(r.Info); info != nil {
		if err := s.devices.UpdateInfo(ctx, id, info); err == nil {
			var v dto.DeviceInfo
			_ = json.Unmarshal(info, &v)
			s.bus.Publish(TopicDeviceUpdated, map[string]any{"id": id, "info": v})
		}
	}
	s.UpdateMetrics(ctx, id, r.Metrics)
}

// 设备鉴权错误（网关据此计入封禁）。
var (
	ErrBadDeviceToken = errors.New("invalid device token")
	ErrTokenBoundElse = errors.New("token bound to another device")
	// ErrTokenRevoked 令牌曾经有效、已被吊销：是我们自己的设备，不计入封禁，并告知设备重新申请接入。
	ErrTokenRevoked = errors.New("device token revoked")
	// ErrTokenUnknown 格式正确但本服务器没有这个令牌（设备换了服务器、或数据库是新的）：
	// 同样告知设备丢弃令牌、重新申请接入，否则设备会拿着旧令牌一直重试。
	ErrTokenUnknown = errors.New("device token unknown")
)

type DeviceService struct {
	repo    *repository.Set
	devices repository.DeviceRepository
	tokens  repository.DeviceTokenRepository
	audit   *AuditService
	bus     *Bus
}

func NewDeviceService(r *repository.Set, audit *AuditService, bus *Bus) *DeviceService {
	return &DeviceService{repo: r, devices: r.Devices, tokens: r.DeviceTokens, audit: audit, bus: bus}
}

// Authenticate 校验 Bearer 令牌，返回令牌记录。
func (s *DeviceService) Authenticate(ctx context.Context, token string) (*model.DeviceToken, error) {
	if len(token) < 20 || len(token) > 128 {
		return nil, ErrBadDeviceToken
	}
	hash := secure.TokenHash(token)
	t, err := s.tokens.FindActiveByHash(ctx, hash)
	if errors.Is(err, repository.ErrNotFound) {
		if _, err := s.tokens.FindByHash(ctx, hash); err == nil {
			return nil, ErrTokenRevoked
		}
		if strings.HasPrefix(token, deviceTokenPrefix) {
			return nil, ErrTokenUnknown
		}
		return nil, ErrBadDeviceToken
	}
	return t, err
}

// OnHello 设备上线：登记设备并把令牌绑定到它（一个令牌只能用于一台设备）。
func (s *DeviceService) OnHello(ctx context.Context, tok *model.DeviceToken, h HelloInfo) (*model.Device, error) {
	if h.DeviceUID == "" || len(h.DeviceUID) > 64 {
		return nil, fmt.Errorf("bad device id")
	}
	cmds, _ := json.Marshal(h.RemoteCommands)
	now := time.Now().UTC()
	d := &model.Device{
		DeviceUID: h.DeviceUID, Name: trunc(h.Name, 64), Version: trunc(h.Version, 32), Online: true, ConnectedAt: &now,
		LastIP: h.IP, Network: trunc(h.Network, 16), RemoteCommands: cmds,
		ConfigSchema: normalizeSchema(h.ConfigSchema), ConfigVersion: max(h.ConfigVersion, 0),
		CommandSchema: normalizeCommandSchema(h.CommandSchema, h.RemoteCommands), Info: normalizeInfo(h.Info),
		FirstSeenAt: now, LastSeenAt: now, CreatedAt: now, UpdatedAt: now,
	}
	// 令牌已绑定到别的设备时，不能借它登记新设备。
	if tok.DeviceID != nil {
		cur, err := s.devices.Get(ctx, *tok.DeviceID)
		if err == nil && cur.DeviceUID != h.DeviceUID {
			return nil, ErrTokenBoundElse
		}
	}
	if err := s.devices.UpsertOnHello(ctx, d); err != nil {
		return nil, err
	}
	ok, err := s.tokens.Bind(ctx, tok.ID, d.ID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrTokenBoundElse
	}
	_ = s.tokens.MarkUsed(ctx, tok.ID, now)
	s.bus.Publish(TopicDeviceOnline, dto.DeviceFrom(d))
	return d, nil
}

func (s *DeviceService) OnDisconnect(ctx context.Context, id uint64) {
	now := time.Now().UTC()
	if err := s.devices.SetOffline(ctx, id, now); err == nil {
		if d, err := s.devices.Get(ctx, id); err == nil {
			s.bus.Publish(TopicDeviceOffline, dto.DeviceFrom(d))
		}
	}
}

// Touch 记录心跳延迟与最近在线时间。
func (s *DeviceService) Touch(ctx context.Context, id uint64, rtt time.Duration) {
	_ = s.devices.Touch(ctx, id, int(rtt.Milliseconds()), time.Now().UTC())
	s.bus.Publish(TopicDeviceUpdated, map[string]any{"id": id, "rtt_ms": rtt.Milliseconds()})
}

// UpdateMetrics 保存心跳上报的资源指标（仅接受小的 JSON 对象）。
func (s *DeviceService) UpdateMetrics(ctx context.Context, id uint64, metrics json.RawMessage) {
	if len(metrics) == 0 || len(metrics) > 4096 || metrics[0] != '{' {
		return
	}
	now := time.Now().UTC()
	if err := s.devices.UpdateMetrics(ctx, id, metrics, now); err == nil {
		s.bus.Publish(TopicDeviceUpdated, map[string]any{"id": id, "metrics": metrics, "metrics_at": now})
	}
}

// ResetPresence 服务启动时把所有设备置为离线（上一进程的连接已不存在）。
func (s *DeviceService) ResetPresence(ctx context.Context) error {
	return s.devices.MarkAllOffline(ctx)
}

// Models 全部设备（数据库模型），供命令目录等内部使用。
func (s *DeviceService) Models(ctx context.Context) ([]model.Device, error) {
	return s.devices.List(ctx)
}

func (s *DeviceService) List(ctx context.Context) ([]dto.Device, error) {
	rows, err := s.devices.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]dto.Device, len(rows))
	for i := range rows {
		out[i] = dto.DeviceFrom(&rows[i])
	}
	return out, nil
}

func (s *DeviceService) Get(ctx context.Context, id uint64) (*model.Device, error) {
	d, err := s.devices.Get(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, errcode.NotFound.WithMessage("设备不存在")
	}
	return d, err
}

func (s *DeviceService) SetAlias(ctx context.Context, a Actor, id uint64, alias string) error {
	err := s.devices.SetAlias(ctx, id, alias)
	if errors.Is(err, repository.ErrNotFound) {
		return errcode.NotFound.WithMessage("设备不存在")
	}
	if err == nil {
		s.audit.Record(ctx, a, AuditDeviceAlias, fmt.Sprint(id), true, map[string]string{"alias": alias})
	}
	return err
}

// Delete 删除离线设备：先吊销它的令牌（设备再连会收到 4401、重新申请接入，需管理员再次批准），
// 再清理它的指令、事件、配置与更新记录。在线设备不能删除，避免删到一半又有新数据写入。
func (s *DeviceService) Delete(ctx context.Context, a Actor, id uint64) error {
	d, err := s.devices.Get(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return errcode.NotFound.WithMessage("设备不存在")
	}
	if err != nil {
		return err
	}
	if d.Online {
		return errcode.Conflict.WithMessage("设备在线，不能删除")
	}
	now := time.Now().UTC()
	revoked, err := s.tokens.RevokeByDevice(ctx, id, now)
	if err != nil {
		return err
	}
	// 先删设备本身和小表，最后才清理大量的历史记录：中途出错时不会留下「历史没了、设备还在」的半截状态；
	// 历史清理失败也只是留下孤立记录，会被保留期清理任务删掉。
	if err := s.repo.DeviceConfigs.Delete(ctx, id); err != nil {
		return err
	}
	if err := s.repo.DeviceUpdates.DeleteByDevice(ctx, id); err != nil {
		return err
	}
	if err := s.devices.Delete(ctx, id); err != nil {
		return err
	}
	for _, del := range []func(context.Context, uint64) (int64, error){s.repo.Invocations.DeleteByDevice, s.repo.Events.DeleteByDevice} {
		for {
			n, err := del(ctx, id)
			if err != nil {
				return err
			}
			if n == 0 {
				break
			}
		}
	}
	s.audit.Record(ctx, a, AuditDeviceDelete, fmt.Sprint(id), true,
		map[string]any{"name": d.Name, "alias": d.Alias, "device_uid": d.DeviceUID, "tokens_revoked": revoked})
	s.bus.Publish(TopicDeviceDeleted, map[string]any{"id": id})
	return nil
}

// ---- 令牌 ----

func (s *DeviceService) IssueToken(ctx context.Context, a Actor, name string) (*dto.IssuedToken, error) {
	plain := secure.RandomToken(deviceTokenPrefix)
	t := &model.DeviceToken{Name: name, TokenHash: secure.TokenHash(plain),
		TokenPrefix: plain[:12], CreatedAt: time.Now().UTC()}
	if a.AdminID != 0 {
		id := a.AdminID
		t.CreatedBy = &id
	}
	if err := s.tokens.Create(ctx, t); err != nil {
		return nil, err
	}
	s.audit.Record(ctx, a, AuditTokenIssue, fmt.Sprint(t.ID), true, map[string]string{"name": name})
	return &dto.IssuedToken{DeviceToken: dto.DeviceTokenFrom(t), Token: plain}, nil
}

func (s *DeviceService) RevokeToken(ctx context.Context, a Actor, id uint64) error {
	err := s.tokens.Revoke(ctx, id, time.Now().UTC())
	if errors.Is(err, repository.ErrNotFound) {
		return errcode.NotFound.WithMessage("令牌不存在或已吊销")
	}
	if err == nil {
		s.audit.Record(ctx, a, AuditTokenRevoke, fmt.Sprint(id), true, nil)
	}
	return err
}

// DeleteToken 删除已吊销的令牌（使用中的须先吊销，避免误删正在连接的设备）。
func (s *DeviceService) DeleteToken(ctx context.Context, a Actor, id uint64) error {
	ok, err := s.tokens.DeleteRevoked(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		return errcode.Conflict.WithMessage("请先吊销再删除")
	}
	s.audit.Record(ctx, a, AuditTokenDelete, fmt.Sprint(id), true, nil)
	return nil
}

// TokenActive 令牌是否存在且未吊销。
func (s *DeviceService) TokenActive(ctx context.Context, id uint64) (bool, error) {
	list, err := s.tokens.List(ctx)
	if err != nil {
		return false, err
	}
	for _, t := range list {
		if t.ID == id {
			return t.RevokedAt == nil, nil
		}
	}
	return false, nil
}

func (s *DeviceService) ListTokens(ctx context.Context) ([]dto.DeviceToken, error) {
	rows, err := s.tokens.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]dto.DeviceToken, len(rows))
	for i := range rows {
		out[i] = dto.DeviceTokenFrom(&rows[i])
	}
	return out, nil
}

func trunc(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}
