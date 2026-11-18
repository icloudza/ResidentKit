package service

import (
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/icloudza/ResidentKit/server/internal/dto"
	"github.com/icloudza/ResidentKit/server/internal/model"
	"github.com/icloudza/ResidentKit/server/internal/pkg/errcode"
	"github.com/icloudza/ResidentKit/server/internal/pkg/secure"
	"github.com/icloudza/ResidentKit/server/internal/release"
	"github.com/icloudza/ResidentKit/server/internal/repository"
)

// 自动接入（docs/enroll.md）的限制。
const (
	enrollTTL        = 10 * time.Minute // 待批准的申请多久没人处理就作废
	enrollMaxPending = 20               // 同时待批准的申请上限，防刷
	enrollPerIPHour  = 20               // 同一 IP 每小时最多发起的新申请（一个出口 IP 后可能有多台设备）
	enrollRejectFor  = time.Hour        // 被拒绝后多久内不再弹出
	enrollClockSkew  = 5 * time.Minute  // 请求时间戳允许的误差

	enrollTicketPrefix    = "rke_"
	enrollTicketMaxActive = 50
)

var (
	enrollActor = Actor{Username: "enroll"}
	deviceUIDRe = regexp.MustCompile(`^[A-Za-z0-9-]{8,64}$`)
	// ErrBadEnroll 签名或格式错误，网关据此计入封禁。错误码独立，便于 errors.Is 区分。
	ErrBadEnroll = errcode.BadEnrollment
)

// EnrollService 设备自动接入：设备用自己的密钥申请，管理员在控制台同意后签发令牌。
//
// 设备每次请求都带私钥签名；同意后由下一次请求领取令牌，签名保证只有发起申请的那台设备拿得到。
type EnrollService struct {
	repo    *repository.Set
	devices *DeviceService
	audit   *AuditService
	bus     *Bus
	log     *zap.Logger

	mu    sync.Mutex
	perIP map[string][]time.Time // 近一小时发起新申请的时间
}

func NewEnrollService(r *repository.Set, devices *DeviceService, audit *AuditService, bus *Bus, log *zap.Logger) *EnrollService {
	return &EnrollService{repo: r, devices: devices, audit: audit, bus: bus, log: log.Named("enroll"),
		perIP: map[string][]time.Time{}}
}

// EnrollMessage 设备签名的内容（与 daemon RKEnrollModule 逐字节一致）。
func EnrollMessage(device, publicKey string, ts int64) []byte {
	return []byte(fmt.Sprintf("residentkit-enroll\nv1\n%s\n%s\n%d\n", device, publicKey, ts))
}

// pairingCode 公钥指纹的 6 位数字，控制台与设备 App 显示同一个，便于核对。
func pairingCode(publicKey string) string {
	sum := sha256.Sum256([]byte(publicKey))
	return fmt.Sprintf("%06d", binary.BigEndian.Uint32(sum[:4])%1000000)
}

// Request 处理设备的接入请求（首次申请与后续轮询是同一个接口）。
func (s *EnrollService) Request(ctx context.Context, ip string, req dto.EnrollRequest) (*dto.EnrollResponse, error) {
	if err := s.verify(req); err != nil {
		return nil, err
	}
	cur, err := s.repo.Enrollments.Latest(ctx, req.Device)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}
	if err == nil && cur.PublicKey == req.PublicKey {
		switch cur.Status {
		case model.EnrollPending:
			if time.Since(cur.CreatedAt) < enrollTTL {
				if t := s.useTicket(ctx, req.Ticket); t != nil {
					return s.approveByTicket(ctx, cur, t, ip)
				}
				return &dto.EnrollResponse{Status: "pending", Code: cur.Code}, nil
			}
			_, _ = s.repo.Enrollments.Transition(ctx, cur.ID, []string{model.EnrollPending}, map[string]any{"status": model.EnrollExpired})
		case model.EnrollApproved:
			return s.issue(ctx, cur, nil)
		case model.EnrollCompleted:
			// 设备丢了令牌（如重装）再来领：令牌仍有效则换发新的；已被吊销说明是管理员踢掉的，须重新批准。
			if cur.TokenID != nil {
				if active, _ := s.devices.TokenActive(ctx, *cur.TokenID); active {
					return s.issue(ctx, cur, cur.TokenID)
				}
			}
		case model.EnrollRejected:
			if cur.DecidedAt != nil && time.Since(*cur.DecidedAt) < enrollRejectFor {
				if t := s.useTicket(ctx, req.Ticket); t != nil { // 管理员又发了接入码：以接入码为准
					return s.createApproved(ctx, ip, req, t)
				}
				left := enrollRejectFor - time.Since(*cur.DecidedAt)
				return &dto.EnrollResponse{Status: "rejected", RetryAfter: int(left.Seconds()) + 1}, nil
			}
		}
	} else if err == nil && cur.Status == model.EnrollPending {
		// 同一设备换了密钥（重装后重新生成）：旧申请作废。
		_, _ = s.repo.Enrollments.Transition(ctx, cur.ID, []string{model.EnrollPending}, map[string]any{"status": model.EnrollExpired})
	}
	if t := s.useTicket(ctx, req.Ticket); t != nil {
		return s.createApproved(ctx, ip, req, t)
	}
	return s.create(ctx, ip, req)
}

// verify 校验格式、时间戳与签名。
func (s *EnrollService) verify(req dto.EnrollRequest) error {
	if !deviceUIDRe.MatchString(req.Device) || req.Name == "" || len(req.Name) > 64 || len(req.Model) > 32 || len(req.Version) > 32 {
		return ErrBadEnroll
	}
	if d := time.Since(time.Unix(req.TS, 0)); d > enrollClockSkew || d < -enrollClockSkew {
		return errcode.InvalidParams.WithMessage("设备时间不准")
	}
	pub, err := release.ParsePublicKey(req.PublicKey)
	if err != nil {
		return ErrBadEnroll
	}
	sig, err := base64.StdEncoding.DecodeString(req.Signature)
	if err != nil {
		return ErrBadEnroll
	}
	sum := sha256.Sum256(EnrollMessage(req.Device, req.PublicKey, req.TS))
	if !ecdsa.VerifyASN1(pub, sum[:], sig) {
		return ErrBadEnroll
	}
	return nil
}

func (s *EnrollService) create(ctx context.Context, ip string, req dto.EnrollRequest) (*dto.EnrollResponse, error) {
	if !s.allowIP(ip) {
		return nil, errcode.TooManyRequests.WithMessage("接入申请过于频繁")
	}
	if n, err := s.repo.Enrollments.CountPending(ctx); err != nil {
		return nil, err
	} else if n >= enrollMaxPending {
		return nil, errcode.TooManyRequests.WithMessage("待批准的接入申请过多")
	}
	now := time.Now().UTC()
	e := &model.Enrollment{DeviceUID: req.Device, Name: trunc(req.Name, 64), Model: trunc(req.Model, 32),
		Version: trunc(req.Version, 32), PublicKey: req.PublicKey, IP: ip, Code: pairingCode(req.PublicKey),
		Status: model.EnrollPending, CreatedAt: now, UpdatedAt: now}
	if err := s.repo.Enrollments.Create(ctx, e); err != nil {
		return nil, err
	}
	s.log.Info("enroll request", zap.String("device", e.DeviceUID), zap.String("ip", ip))
	s.publish(e)
	return &dto.EnrollResponse{Status: "pending", Code: e.Code}, nil
}

func (s *EnrollService) allowIP(ip string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	cut := time.Now().Add(-time.Hour)
	list := s.perIP[ip][:0]
	for _, t := range s.perIP[ip] {
		if t.After(cut) {
			list = append(list, t)
		}
	}
	if len(list) >= enrollPerIPHour {
		s.perIP[ip] = list
		return false
	}
	s.perIP[ip] = append(list, time.Now())
	return true
}

// issue 签发令牌给已批准的设备；old 不为空时先吊销旧令牌（换发）。
func (s *EnrollService) issue(ctx context.Context, e *model.Enrollment, old *uint64) (*dto.EnrollResponse, error) {
	t, err := s.devices.IssueToken(ctx, Actor{Username: enrollActor.Username, IP: e.IP}, e.Name+"（自动接入）")
	if err != nil {
		return nil, err
	}
	// 条件更新保证同一次批准只发出一个令牌：首次领取只能从 approved 转出；
	// 换发只能在 token_id 仍是旧令牌时进行。并发的第二个请求更新不到行，其令牌立即吊销。
	var ok bool
	if old == nil {
		ok, err = s.repo.Enrollments.Transition(ctx, e.ID, []string{model.EnrollApproved},
			map[string]any{"status": model.EnrollCompleted, "token_id": t.ID})
	} else {
		ok, err = s.repo.Enrollments.SwapToken(ctx, e.ID, *old, t.ID)
	}
	if err != nil || !ok {
		_ = s.devices.RevokeToken(ctx, enrollActor, t.ID)
		if err == nil {
			err = errcode.Conflict.WithMessage("接入状态已变化，请重试")
		}
		return nil, err
	}
	if old != nil {
		_ = s.devices.RevokeToken(ctx, enrollActor, *old)
	}
	e.Status = model.EnrollCompleted
	s.publish(e)
	return &dto.EnrollResponse{Status: "approved", Token: t.Token}, nil
}

// ---- 接入码 ----

// useTicket 核对接入码并占用一次；无效（不存在、过期、用完、已吊销）返回 nil，按普通申请处理。
// 不计入封禁：设备会带着同一个码反复轮询，无效码若计入封禁，设备很快就被封。
func (s *EnrollService) useTicket(ctx context.Context, token string) *model.EnrollTicket {
	if token == "" {
		return nil
	}
	if !strings.HasPrefix(token, enrollTicketPrefix) || len(token) > 128 {
		s.log.Info("enroll ticket malformed")
		return nil
	}
	t, err := s.repo.EnrollTickets.FindByHash(ctx, secure.TokenHash(token))
	if err != nil {
		s.log.Info("enroll ticket unknown")
		return nil
	}
	ok, err := s.repo.EnrollTickets.TakeUse(ctx, t.ID, time.Now().UTC())
	if err != nil || !ok {
		s.log.Info("enroll ticket not usable", zap.Uint64("ticket", t.ID), zap.String("status", dto.EnrollTicketStatus(t, time.Now().UTC())))
		return nil
	}
	return t
}

// approveByTicket 待批准的申请凭接入码直接批准并签发令牌。
func (s *EnrollService) approveByTicket(ctx context.Context, e *model.Enrollment, t *model.EnrollTicket, ip string) (*dto.EnrollResponse, error) {
	now := time.Now().UTC()
	ok, err := s.repo.Enrollments.Transition(ctx, e.ID, []string{model.EnrollPending},
		map[string]any{"status": model.EnrollApproved, "decided_at": now, "ticket_id": t.ID})
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errcode.Conflict.WithMessage("接入状态已变化，请重试")
	}
	e.Status, e.DecidedAt, e.TicketID = model.EnrollApproved, &now, &t.ID
	s.recordTicketApprove(ctx, e, t, ip)
	return s.issue(ctx, e, nil)
}

// createApproved 凭接入码新建一条已批准的申请并签发令牌（不受待批准数量与每 IP 频率限制，接入码自带次数限制）。
func (s *EnrollService) createApproved(ctx context.Context, ip string, req dto.EnrollRequest, t *model.EnrollTicket) (*dto.EnrollResponse, error) {
	now := time.Now().UTC()
	e := &model.Enrollment{DeviceUID: req.Device, Name: trunc(req.Name, 64), Model: trunc(req.Model, 32),
		Version: trunc(req.Version, 32), PublicKey: req.PublicKey, IP: ip, Code: pairingCode(req.PublicKey),
		Status: model.EnrollApproved, DecidedAt: &now, TicketID: &t.ID, CreatedAt: now, UpdatedAt: now}
	if err := s.repo.Enrollments.Create(ctx, e); err != nil {
		return nil, err
	}
	s.recordTicketApprove(ctx, e, t, ip)
	return s.issue(ctx, e, nil)
}

func (s *EnrollService) recordTicketApprove(ctx context.Context, e *model.Enrollment, t *model.EnrollTicket, ip string) {
	s.log.Info("enroll approved by ticket", zap.String("device", e.DeviceUID), zap.Uint64("ticket", t.ID), zap.String("ip", ip))
	s.audit.Record(ctx, Actor{Username: "接入码 · " + t.Name, IP: ip}, AuditEnrollApprove, e.DeviceUID, true,
		map[string]any{"name": e.Name, "model": e.Model, "ip": ip, "ticket": t.ID})
}

// Tickets 全部接入码（新到旧）。
func (s *EnrollService) Tickets(ctx context.Context) ([]dto.EnrollTicket, error) {
	list, err := s.repo.EnrollTickets.List(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	out := make([]dto.EnrollTicket, 0, len(list))
	for i := range list {
		out = append(out, dto.EnrollTicketFrom(&list[i], now))
	}
	return out, nil
}

// CreateTicket 生成接入码（明文只返回这一次）。生成接入码相当于预先点了「同意」，与批准申请同一权限，不再次验证密码。
func (s *EnrollService) CreateTicket(ctx context.Context, a Actor, req dto.CreateEnrollTicketRequest) (*dto.IssuedEnrollTicket, error) {
	list, err := s.repo.EnrollTickets.List(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	active := 0
	for i := range list {
		if dto.EnrollTicketStatus(&list[i], now) == "active" {
			active++
		}
	}
	if active >= enrollTicketMaxActive {
		return nil, errcode.Conflict.WithMessage("可用的接入码已达上限 %d 个，请先吊销不用的", enrollTicketMaxActive)
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "新设备"
	}
	plain := secure.RandomToken(enrollTicketPrefix)
	t := &model.EnrollTicket{Name: trunc(name, 64), TokenHash: secure.TokenHash(plain), TokenPrefix: plain[:12],
		ExpiresAt: now.Add(time.Duration(req.ExpiresMinutes) * time.Minute), MaxUses: uint32(req.MaxUses), CreatedAt: now}
	if a.AdminID != 0 {
		id := a.AdminID
		t.CreatedBy = &id
	}
	if err := s.repo.EnrollTickets.Create(ctx, t); err != nil {
		return nil, err
	}
	s.audit.Record(ctx, a, AuditTicketCreate, t.Name, true,
		map[string]any{"ticket": t.ID, "expires_minutes": req.ExpiresMinutes, "max_uses": req.MaxUses})
	return &dto.IssuedEnrollTicket{EnrollTicket: dto.EnrollTicketFrom(t, now), Token: plain}, nil
}

func (s *EnrollService) RevokeTicket(ctx context.Context, a Actor, id uint64) error {
	t, err := s.repo.EnrollTickets.Get(ctx, id)
	if err != nil {
		return errcode.NotFound
	}
	if err := s.repo.EnrollTickets.Revoke(ctx, id, time.Now().UTC()); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return errcode.Conflict.WithMessage("接入码已吊销")
		}
		return err
	}
	s.audit.Record(ctx, a, AuditTicketRevoke, t.Name, true, map[string]any{"ticket": id})
	return nil
}

// Decide 管理员同意或拒绝。
func (s *EnrollService) Decide(ctx context.Context, a Actor, id uint64, approve bool) error {
	to, action := model.EnrollRejected, AuditEnrollReject
	if approve {
		to, action = model.EnrollApproved, AuditEnrollApprove
	}
	now := time.Now().UTC()
	updates := map[string]any{"status": to, "decided_at": now}
	if a.AdminID != 0 {
		updates["decided_by"] = a.AdminID
	}
	ok, err := s.repo.Enrollments.Transition(ctx, id, []string{model.EnrollPending}, updates)
	if err != nil {
		return err
	}
	if !ok {
		return errcode.Conflict.WithMessage("该申请已处理或已过期")
	}
	e, err := s.repo.Enrollments.Get(ctx, id)
	if err == nil {
		s.audit.Record(ctx, a, action, e.DeviceUID, true, map[string]string{"name": e.Name, "model": e.Model, "ip": e.IP})
		s.publish(e)
	}
	return nil
}

// Pending 待批准的申请（顺带清理过期的）。
func (s *EnrollService) Pending(ctx context.Context) ([]dto.Enrollment, error) {
	s.Expire(ctx)
	list, err := s.repo.Enrollments.Pending(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]dto.Enrollment, len(list))
	for i := range list {
		out[i] = dto.EnrollmentFrom(&list[i])
	}
	return out, nil
}

// GC 定时清理按 IP 的申请计数，防止大量不同 IP 让它无限增长。
func (s *EnrollService) GC() {
	s.mu.Lock()
	defer s.mu.Unlock()
	cut := time.Now().Add(-time.Hour)
	for ip, list := range s.perIP {
		if len(list) == 0 || list[len(list)-1].Before(cut) {
			delete(s.perIP, ip)
		}
	}
}

// Expire 定时任务：超时未处理的申请作废。
func (s *EnrollService) Expire(ctx context.Context) {
	if n, err := s.repo.Enrollments.ExpireBefore(ctx, time.Now().UTC().Add(-enrollTTL)); err == nil && n > 0 {
		s.bus.Publish(TopicEnrollChanged, map[string]any{"expired": n})
	}
}

func (s *EnrollService) publish(e *model.Enrollment) {
	s.bus.Publish(TopicEnrollChanged, dto.EnrollmentFrom(e))
}
