package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/icloudza/ResidentKit/server/internal/dto"
	"github.com/icloudza/ResidentKit/server/internal/model"
	"github.com/icloudza/ResidentKit/server/internal/pkg/errcode"
	"github.com/icloudza/ResidentKit/server/internal/pkg/secure"
	"github.com/icloudza/ResidentKit/server/internal/repository"
)

const (
	apiKeyPrefix    = "rkk_"
	apiKeyCacheTTL  = 15 * time.Second // 校验结果缓存：高并发时不必每个请求都查库；吊销时立即清除
	apiKeyTouchGap  = time.Minute      // 「最近使用」最多每分钟写一次，避免并发请求争抢同一行
	apiKeyMaxActive = 100
)

var allScopes = []string{model.ScopeDevicesRead, model.ScopeCommandsInvoke, model.ScopeEventsRead}

// ErrBadAPIKey 密钥无效、已吊销或已过期（网关据此计入封禁）。
var ErrBadAPIKey = errcode.Unauthorized.WithMessage("API 密钥无效")

// APIPrincipal 通过校验的调用方。
type APIPrincipal struct {
	KeyID   uint64
	Name    string
	scopes  []string
	devices []uint64 // nil 表示全部设备
}

func (p *APIPrincipal) Has(scope string) bool { return slices.Contains(p.scopes, scope) }

// CanAccess 设备是否在该密钥的范围内。
func (p *APIPrincipal) CanAccess(deviceID uint64) bool {
	return p.devices == nil || slices.Contains(p.devices, deviceID)
}

// Actor 审计用：操作者记为「api:密钥名」。
func (p *APIPrincipal) Actor(ip string) Actor {
	return Actor{Username: "api:" + p.Name, IP: ip}
}

type cachedKey struct {
	key *model.APIKey
	at  time.Time
}

// APIKeyService 开放 API 的密钥：签发、校验、吊销（docs/open-api.md）。
type APIKeyService struct {
	repo  repository.APIKeyRepository
	auth  *AuthService
	audit *AuditService
	log   *zap.Logger

	mu      sync.Mutex
	cache   map[string]cachedKey // 令牌哈希 → 密钥
	touched map[uint64]time.Time
}

func NewAPIKeyService(r *repository.Set, auth *AuthService, audit *AuditService, log *zap.Logger) *APIKeyService {
	return &APIKeyService{repo: r.APIKeys, auth: auth, audit: audit, log: log.Named("apikey"),
		cache: map[string]cachedKey{}, touched: map[uint64]time.Time{}}
}

// Create 签发密钥（需再次验证密码），明文只在返回值里出现这一次。
func (s *APIKeyService) Create(ctx context.Context, a Actor, req dto.CreateAPIKeyRequest) (*dto.IssuedAPIKey, error) {
	if err := s.auth.Reauth(ctx, a, req.Password, req.TOTPCode); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, errcode.InvalidParams.WithMessage("请填写名称")
	}
	if len(req.Scopes) == 0 {
		return nil, errcode.InvalidParams.WithMessage("至少选择一项权限")
	}
	for _, sc := range req.Scopes {
		if !slices.Contains(allScopes, sc) {
			return nil, errcode.InvalidParams.WithMessage("未知权限 %s", sc)
		}
	}
	for _, ip := range req.AllowedIPs {
		if _, err := parseIPRule(ip); err != nil {
			return nil, errcode.InvalidParams.WithMessage("IP 白名单格式错误：%s", ip)
		}
	}
	if n, err := s.repo.CountActive(ctx); err != nil {
		return nil, err
	} else if n >= apiKeyMaxActive {
		return nil, errcode.Conflict.WithMessage("有效密钥已达上限 %d 个", apiKeyMaxActive)
	}
	plain := secure.RandomToken(apiKeyPrefix)
	now := time.Now().UTC()
	k := &model.APIKey{Name: trunc(name, 64), KeyHash: secure.TokenHash(plain), KeyPrefix: plain[:12], CreatedAt: now}
	k.Scopes, _ = json.Marshal(slices.Compact(slices.Sorted(slices.Values(req.Scopes))))
	if len(req.DeviceIDs) > 0 {
		k.DeviceIDs, _ = json.Marshal(slices.Compact(slices.Sorted(slices.Values(req.DeviceIDs))))
	}
	if len(req.AllowedIPs) > 0 {
		k.AllowedIPs, _ = json.Marshal(req.AllowedIPs)
	}
	if req.ExpiresDays > 0 {
		t := now.Add(time.Duration(req.ExpiresDays) * 24 * time.Hour)
		k.ExpiresAt = &t
	}
	if a.AdminID != 0 {
		id := a.AdminID
		k.CreatedBy = &id
	}
	if err := s.repo.Create(ctx, k); err != nil {
		return nil, err
	}
	s.audit.Record(ctx, a, AuditAPIKeyCreate, fmt.Sprint(k.ID), true,
		map[string]any{"name": k.Name, "scopes": req.Scopes, "devices": req.DeviceIDs})
	return &dto.IssuedAPIKey{APIKey: dto.APIKeyFrom(k), Key: plain}, nil
}

func (s *APIKeyService) List(ctx context.Context) ([]dto.APIKey, error) {
	rows, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]dto.APIKey, len(rows))
	for i := range rows {
		out[i] = dto.APIKeyFrom(&rows[i])
	}
	return out, nil
}

// Revoke 吊销并立即清除缓存，之后的请求马上失效。
func (s *APIKeyService) Revoke(ctx context.Context, a Actor, id uint64) error {
	err := s.repo.Revoke(ctx, id, time.Now().UTC())
	if errors.Is(err, repository.ErrNotFound) {
		return errcode.NotFound.WithMessage("密钥不存在或已吊销")
	}
	if err != nil {
		return err
	}
	s.dropCache(id)
	s.audit.Record(ctx, a, AuditAPIKeyRevoke, fmt.Sprint(id), true, nil)
	return nil
}

func (s *APIKeyService) Delete(ctx context.Context, a Actor, id uint64) error {
	ok, err := s.repo.DeleteRevoked(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		return errcode.Conflict.WithMessage("请先吊销再删除")
	}
	s.audit.Record(ctx, a, AuditAPIKeyDelete, fmt.Sprint(id), true, nil)
	return nil
}

func (s *APIKeyService) dropCache(id uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for h, c := range s.cache {
		if c.key.ID == id {
			delete(s.cache, h)
		}
	}
}

// Authenticate 校验 Bearer 密钥：存在、未吊销、未过期、来源 IP 在白名单内。并发安全。
func (s *APIKeyService) Authenticate(ctx context.Context, token, ip string) (*APIPrincipal, error) {
	if !strings.HasPrefix(token, apiKeyPrefix) || len(token) < 20 || len(token) > 128 {
		return nil, ErrBadAPIKey
	}
	hash := secure.TokenHash(token)
	now := time.Now()

	s.mu.Lock()
	c, ok := s.cache[hash]
	s.mu.Unlock()
	if !ok || now.Sub(c.at) > apiKeyCacheTTL {
		k, err := s.repo.FindActiveByHash(ctx, hash)
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrBadAPIKey
		}
		if err != nil {
			return nil, err
		}
		c = cachedKey{key: k, at: now}
		s.mu.Lock()
		if len(s.cache) > 1000 {
			s.cache = map[string]cachedKey{} // 防止被大量不同的无效值撑大（只缓存有效密钥，通常很少）
		}
		s.cache[hash] = c
		s.mu.Unlock()
	}
	k := c.key
	if k.ExpiresAt != nil && now.After(*k.ExpiresAt) {
		return nil, ErrBadAPIKey
	}
	if !ipAllowed(k.AllowedIPs, ip) {
		return nil, errcode.Forbidden.WithMessage("当前 IP 不在白名单内")
	}
	s.touch(k.ID, ip)

	return principalOf(k), nil
}

func principalOf(k *model.APIKey) *APIPrincipal {
	p := &APIPrincipal{KeyID: k.ID, Name: k.Name}
	_ = json.Unmarshal(k.Scopes, &p.scopes)
	if len(k.DeviceIDs) > 0 {
		var ids []uint64
		if json.Unmarshal(k.DeviceIDs, &ids) == nil && len(ids) > 0 {
			p.devices = ids
		}
	}
	return p
}

// PrincipalForKey 控制台「在线调试」：管理员按密钥 ID 选择要模拟的密钥（不经明文、不校验来源 IP）。
// 仍要求密钥有效（未吊销、未过期），权限与设备范围照常生效。
func (s *APIKeyService) PrincipalForKey(ctx context.Context, id uint64) (*APIPrincipal, error) {
	k, err := s.repo.Get(ctx, id)
	if err != nil || k.RevokedAt != nil {
		return nil, errcode.NotFound.WithMessage("密钥不存在或已吊销")
	}
	if k.ExpiresAt != nil && time.Now().After(*k.ExpiresAt) {
		return nil, errcode.Conflict.WithMessage("密钥已过期")
	}
	return principalOf(k), nil
}

// touch 记录最近使用（节流，后台写入，不阻塞请求）。
func (s *APIKeyService) touch(id uint64, ip string) {
	now := time.Now()
	s.mu.Lock()
	if now.Sub(s.touched[id]) < apiKeyTouchGap {
		s.mu.Unlock()
		return
	}
	s.touched[id] = now
	s.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.repo.Touch(ctx, id, now.UTC(), trunc(ip, 45)); err != nil {
			s.log.Warn("touch api key", zap.Error(err))
		}
	}()
}

func parseIPRule(s string) (netip.Prefix, error) {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "/") {
		return netip.ParsePrefix(s)
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, err
	}
	return netip.PrefixFrom(a, a.BitLen()), nil
}

func ipAllowed(rules []byte, ip string) bool {
	var list []string
	if len(rules) == 0 || json.Unmarshal(rules, &list) != nil || len(list) == 0 {
		return true
	}
	a, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	for _, r := range list {
		if p, err := parseIPRule(r); err == nil && p.Contains(a.Unmap()) {
			return true
		}
	}
	return false
}

// Update 编辑密钥的名称、权限、设备范围、IP 白名单与有效期（需再次验证密码），立即生效。
func (s *APIKeyService) Update(ctx context.Context, a Actor, id uint64, req dto.UpdateAPIKeyRequest) (*dto.APIKey, error) {
	if err := s.auth.Reauth(ctx, a, req.Password, req.TOTPCode); err != nil {
		return nil, err
	}
	k, err := s.repo.Get(ctx, id)
	if errors.Is(err, repository.ErrNotFound) || (err == nil && k.RevokedAt != nil) {
		return nil, errcode.NotFound.WithMessage("密钥不存在或已吊销")
	}
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, errcode.InvalidParams.WithMessage("请填写名称")
	}
	for _, sc := range req.Scopes {
		if !slices.Contains(allScopes, sc) {
			return nil, errcode.InvalidParams.WithMessage("未知权限 %s", sc)
		}
	}
	for _, ip := range req.AllowedIPs {
		if _, err := parseIPRule(ip); err != nil {
			return nil, errcode.InvalidParams.WithMessage("IP 白名单格式错误：%s", ip)
		}
	}
	updates := map[string]any{"name": trunc(name, 64), "device_ids": nil, "allowed_ips": nil}
	updates["scopes"], _ = json.Marshal(slices.Compact(slices.Sorted(slices.Values(req.Scopes))))
	if len(req.DeviceIDs) > 0 {
		b, _ := json.Marshal(slices.Compact(slices.Sorted(slices.Values(req.DeviceIDs))))
		updates["device_ids"] = b
	}
	if len(req.AllowedIPs) > 0 {
		b, _ := json.Marshal(req.AllowedIPs)
		updates["allowed_ips"] = b
	}
	if req.ExpiresDays != nil {
		if *req.ExpiresDays == 0 {
			updates["expires_at"] = nil
		} else {
			updates["expires_at"] = time.Now().UTC().Add(time.Duration(*req.ExpiresDays) * 24 * time.Hour)
		}
	}
	if err := s.repo.Update(ctx, id, updates); err != nil {
		return nil, err
	}
	s.dropCache(id)
	s.audit.Record(ctx, a, AuditAPIKeyUpdate, fmt.Sprint(id), true,
		map[string]any{"name": name, "scopes": req.Scopes, "devices": req.DeviceIDs, "ips": req.AllowedIPs, "expires_days": req.ExpiresDays})
	k, err = s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	out := dto.APIKeyFrom(k)
	return &out, nil
}

// Calls 开放 API 调用记录（新到旧）。
func (s *APIKeyService) Calls(ctx context.Context, stats repository.APIStatsRepository, f repository.APICallFilter) (dto.APICallPage, error) {
	rows, total, err := stats.List(ctx, f)
	if err != nil {
		return dto.APICallPage{}, err
	}
	out := dto.APICallPage{Items: make([]dto.Invocation, len(rows)), Total: total, Page: f.Num(), PageSize: f.Size()}
	for i := range rows {
		out.Items[i] = dto.InvocationFrom(&rows[i])
	}
	return out, nil
}

// Stats 近 24 小时概况：总量与环比、状态分布、耗时、每小时走势、热门命令、每个密钥的走势。
func (s *APIKeyService) Stats(ctx context.Context, stats repository.APIStatsRepository) (*dto.APIStats, error) {
	now := time.Now().UTC()
	start := now.Truncate(time.Hour).Add(-23 * time.Hour)
	out := &dto.APIStats{Hourly: make([]dto.APIHour, 24), Top: []dto.APICommandCount{}, PerKey: map[uint64]dto.APIKeyStat{}}

	hours, err := stats.Hourly(ctx, start)
	if err != nil {
		return nil, err
	}
	idx := map[string]int{}
	for i := range 24 {
		h := start.Add(time.Duration(i) * time.Hour)
		out.Hourly[i].Hour = h
		idx[h.Format("2006-01-02T15:00:00Z")] = i
	}
	for _, h := range hours {
		i, ok := idx[h.Hour]
		if !ok {
			continue
		}
		out.Hourly[i].Total += h.Total
		out.Hourly[i].OK += h.OK
		out.Hourly[i].Bad += h.Bad
		ks := out.PerKey[h.KeyID]
		if ks.Hourly == nil {
			ks.Hourly = make([]int64, 24)
		}
		ks.Hourly[i] += h.Total
		ks.Calls += h.Total
		ks.OK += h.OK
		out.PerKey[h.KeyID] = ks
	}
	out.UsedKeys = int64(len(out.PerKey))

	st, err := stats.StatusCounts(ctx, start)
	if err != nil {
		return nil, err
	}
	for status, n := range st {
		out.Calls += n
		switch status {
		case model.InvSucceeded:
			out.Succeeded += n
		case model.InvFailed:
			out.Failed += n
		case model.InvLost, model.InvExpired:
			out.Lost += n
		default:
			out.Running += n
		}
	}
	if out.CallsPrev, err = stats.Count(ctx, start.Add(-24*time.Hour), start); err != nil {
		return nil, err
	}
	if out.AvgMs, out.P95Ms, err = stats.Latency(ctx, start); err != nil {
		return nil, err
	}
	top, err := stats.TopCommands(ctx, start, 5)
	if err != nil {
		return nil, err
	}
	for _, t := range top {
		out.Top = append(out.Top, dto.APICommandCount{Module: t.Module, Command: t.Command, Count: t.N})
	}
	if out.ActiveKeys, err = s.repo.CountActive(ctx); err != nil {
		return nil, err
	}
	return out, nil
}
