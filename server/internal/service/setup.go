package service

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"regexp"
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

var usernameRe = regexp.MustCompile(`^[A-Za-z0-9_.-]{3,32}$`)

// SetupService 首次初始化：系统中没有管理员时，凭一次性初始化码创建第一个管理员。
//
// 初始化码在服务启动时生成，只写入服务器日志，防止控制台暴露在公网时被他人抢先初始化。
type SetupService struct {
	admins repository.AdminRepository
	auth   *AuthService
	audit  *AuditService
	log    *zap.Logger

	mu   sync.Mutex
	code string // 空表示已初始化
}

func NewSetupService(r *repository.Set, auth *AuthService, audit *AuditService, log *zap.Logger) *SetupService {
	return &SetupService{admins: r.Admins, auth: auth, audit: audit, log: log}
}

// Prepare 启动时调用：尚无管理员则生成初始化码并写入日志。
func (s *SetupService) Prepare(ctx context.Context) error {
	n, err := s.admins.Count(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	s.mu.Lock()
	s.code = newSetupCode()
	code := s.code
	s.mu.Unlock()
	s.log.Warn("系统尚未初始化，请在控制台完成首次设置", zap.String("初始化码", code))
	return nil
}

// NeedsSetup 是否处于待初始化状态。
func (s *SetupService) NeedsSetup() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.code != ""
}

// Complete 校验初始化码并创建第一个管理员，成功后直接登录。
func (s *SetupService) Complete(ctx context.Context, req dto.SetupRequest, ip, ua string) (*Session, error) {
	actor := Actor{Username: req.Username, IP: ip}
	s.mu.Lock()
	defer s.mu.Unlock() // 串行化，避免并发初始化出两个管理员
	if s.code == "" {
		return nil, errcode.Conflict.WithMessage("系统已初始化")
	}
	given := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(req.Code), "-", ""))
	want := strings.ReplaceAll(s.code, "-", "")
	if subtle.ConstantTimeCompare([]byte(given), []byte(want)) != 1 {
		s.audit.Record(ctx, actor, AuditSetup, req.Username, false, map[string]string{"reason": "bad code"})
		return nil, errcode.InvalidParams.WithMessage("初始化码错误")
	}
	if !usernameRe.MatchString(req.Username) {
		return nil, errcode.InvalidParams.WithMessage("用户名为 3–32 位字母、数字或 _ . -")
	}
	if len(req.Password) < minPasswordLen {
		return nil, errcode.InvalidParams.WithMessage("密码至少 8 位")
	}
	// 以数据库为准再确认一次（例如期间有人用命令行创建了管理员）。
	if n, err := s.admins.Count(ctx); err != nil {
		return nil, err
	} else if n > 0 {
		s.code = ""
		return nil, errcode.Conflict.WithMessage("系统已初始化")
	}
	hash, err := secure.HashPassword(req.Password)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	a := &model.Admin{Username: req.Username, PasswordHash: hash, Role: "admin",
		LastLoginAt: &now, LastLoginIP: ip, CreatedAt: now, UpdatedAt: now}
	if err := s.admins.Create(ctx, a); err != nil {
		return nil, err
	}
	s.code = ""
	actor.AdminID = a.ID
	s.audit.Record(ctx, actor, AuditSetup, a.Username, true, nil)
	s.log.Info("首次初始化完成", zap.String("admin", a.Username))
	return s.auth.issue(ctx, a, ip, ua)
}

// newSetupCode 12 位大写字母数字（去掉易混字符），按 4 位分组。
func newSetupCode() string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	var sb strings.Builder
	for i, c := range b {
		if i > 0 && i%4 == 0 {
			sb.WriteByte('-')
		}
		sb.WriteByte(alphabet[int(c)%len(alphabet)])
	}
	return sb.String()
}
