package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/pquerna/otp/totp"

	"github.com/icloudza/ResidentKit/server/internal/dto"
	"github.com/icloudza/ResidentKit/server/internal/model"
	"github.com/icloudza/ResidentKit/server/internal/pkg/errcode"
	"github.com/icloudza/ResidentKit/server/internal/pkg/secure"
	"github.com/icloudza/ResidentKit/server/internal/repository"
)

const (
	minPasswordLen     = 8 // 另有失败锁定与登录限流兜底
	refreshTokenPrefix = "rkr_"
	jwtIssuer          = "rk-server"
)

// Claims 访问令牌内容。
type Claims struct {
	Username string `json:"usr"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

// AuthOptions 认证参数。
type AuthOptions struct {
	JWTKey      []byte
	AccessTTL   time.Duration
	RefreshTTL  time.Duration
	MaxFailures int
	LockFor     time.Duration
}

type AuthService struct {
	admins  repository.AdminRepository
	refresh repository.RefreshTokenRepository
	audit   *AuditService
	sealer  *secure.Sealer
	opt     AuthOptions
}

func NewAuthService(r *repository.Set, audit *AuditService, sealer *secure.Sealer, opt AuthOptions) *AuthService {
	return &AuthService{admins: r.Admins, refresh: r.RefreshTokens, audit: audit, sealer: sealer, opt: opt}
}

// Session 登录或刷新的结果；RefreshToken 只通过 HttpOnly Cookie 下发。
type Session struct {
	dto.TokenResponse
	RefreshToken   string
	RefreshExpires time.Time
}

// Login 校验密码与两步验证码，失败多次临时锁定账号。
func (s *AuthService) Login(ctx context.Context, req dto.LoginRequest, ip, ua string) (*Session, error) {
	actor := Actor{Username: req.Username, IP: ip}
	a, err := s.admins.GetByUsername(ctx, req.Username)
	if errors.Is(err, repository.ErrNotFound) {
		secure.DummyVerify()
		s.audit.Record(ctx, actor, AuditLogin, req.Username, false, map[string]string{"reason": "unknown user"})
		return nil, errcode.BadCredentials
	}
	if err != nil {
		return nil, err
	}
	actor.AdminID = a.ID
	now := time.Now().UTC()
	if a.LockedUntil != nil && a.LockedUntil.After(now) {
		s.audit.Record(ctx, actor, AuditLogin, a.Username, false, map[string]string{"reason": "locked"})
		return nil, errcode.AccountLocked.WithMessage("账号已临时锁定，请 %d 分钟后再试",
			int(a.LockedUntil.Sub(now).Minutes())+1)
	}
	if !secure.VerifyPassword(req.Password, a.PasswordHash) {
		s.fail(ctx, a, actor, "bad password")
		return nil, errcode.BadCredentials
	}
	if a.TOTPEnabled {
		if req.TOTPCode == "" {
			return nil, errcode.TOTPRequired
		}
		if !s.checkTOTP(a, req.TOTPCode) {
			s.fail(ctx, a, actor, "bad totp")
			return nil, errcode.TOTPInvalid
		}
	}
	_ = s.admins.Update(ctx, a.ID, map[string]any{"failed_count": 0, "locked_until": nil,
		"last_login_at": now, "last_login_ip": ip, "updated_at": now})
	s.audit.Record(ctx, actor, AuditLogin, a.Username, true, nil)
	a.LastLoginAt, a.LastLoginIP = &now, ip
	return s.issue(ctx, a, ip, ua)
}

func (s *AuthService) fail(ctx context.Context, a *model.Admin, actor Actor, reason string) {
	n := a.FailedCount + 1
	updates := map[string]any{"failed_count": n, "updated_at": time.Now().UTC()}
	if n >= s.opt.MaxFailures {
		updates["locked_until"] = time.Now().UTC().Add(s.opt.LockFor)
		updates["failed_count"] = 0
		reason += ", locked"
	}
	_ = s.admins.Update(ctx, a.ID, updates)
	s.audit.Record(ctx, actor, AuditLogin, a.Username, false, map[string]string{"reason": reason})
}

// Refresh 用刷新令牌换新的访问令牌，并轮换刷新令牌（旧的立即作废）。
func (s *AuthService) Refresh(ctx context.Context, refreshToken, ip, ua string) (*Session, error) {
	now := time.Now().UTC()
	rt, err := s.refresh.FindActive(ctx, secure.TokenHash(refreshToken), now)
	if err != nil {
		return nil, errcode.Unauthorized
	}
	_ = s.refresh.Revoke(ctx, rt.ID, now)
	a, err := s.admins.Get(ctx, rt.AdminID)
	if err != nil {
		return nil, errcode.Unauthorized
	}
	return s.issue(ctx, a, ip, ua)
}

func (s *AuthService) Logout(ctx context.Context, actor Actor, refreshToken string) {
	if rt, err := s.refresh.FindActive(ctx, secure.TokenHash(refreshToken), time.Now().UTC()); err == nil {
		_ = s.refresh.Revoke(ctx, rt.ID, time.Now().UTC())
	}
	s.audit.Record(ctx, actor, AuditLogout, actor.Username, true, nil)
}

func (s *AuthService) issue(ctx context.Context, a *model.Admin, ip, ua string) (*Session, error) {
	now := time.Now().UTC()
	exp := now.Add(s.opt.AccessTTL)
	claims := Claims{Username: a.Username, Role: a.Role, RegisteredClaims: jwt.RegisteredClaims{
		Issuer: jwtIssuer, Subject: fmt.Sprint(a.ID),
		IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(exp),
	}}
	access, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.opt.JWTKey)
	if err != nil {
		return nil, err
	}
	plain := secure.RandomToken(refreshTokenPrefix)
	rt := &model.RefreshToken{AdminID: a.ID, TokenHash: secure.TokenHash(plain), UserAgent: trunc(ua, 255),
		IP: ip, ExpiresAt: now.Add(s.opt.RefreshTTL), CreatedAt: now}
	if err := s.refresh.Create(ctx, rt); err != nil {
		return nil, err
	}
	return &Session{
		TokenResponse:  dto.TokenResponse{AccessToken: access, ExpiresAt: exp, Admin: dto.AdminFrom(a)},
		RefreshToken:   plain,
		RefreshExpires: rt.ExpiresAt,
	}, nil
}

// ParseAccessToken 校验访问令牌，只接受 HS256。
func (s *AuthService) ParseAccessToken(token string) (*Claims, error) {
	c := &Claims{}
	_, err := jwt.ParseWithClaims(token, c, func(t *jwt.Token) (any, error) { return s.opt.JWTKey, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithIssuer(jwtIssuer),
		jwt.WithExpirationRequired())
	if err != nil {
		return nil, errcode.Unauthorized
	}
	return c, nil
}

func (s *AuthService) Me(ctx context.Context, id uint64) (*dto.Admin, error) {
	a, err := s.admins.Get(ctx, id)
	if err != nil {
		return nil, errcode.Unauthorized
	}
	out := dto.AdminFrom(a)
	return &out, nil
}

// ChangePassword 修改密码并吊销该账号所有刷新令牌（其他会话需重新登录）。
func (s *AuthService) ChangePassword(ctx context.Context, actor Actor, req dto.ChangePasswordRequest) error {
	a, err := s.admins.Get(ctx, actor.AdminID)
	if err != nil {
		return errcode.Unauthorized
	}
	if !secure.VerifyPassword(req.OldPassword, a.PasswordHash) {
		s.audit.Record(ctx, actor, AuditPasswordChange, a.Username, false, nil)
		return errcode.BadCredentials.WithMessage("原密码错误")
	}
	hash, err := secure.HashPassword(req.NewPassword)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if err := s.admins.Update(ctx, a.ID, map[string]any{"password_hash": hash, "updated_at": now}); err != nil {
		return err
	}
	_ = s.refresh.RevokeAllForAdmin(ctx, a.ID, now)
	s.audit.Record(ctx, actor, AuditPasswordChange, a.Username, true, nil)
	return nil
}

// ChangeUsername 修改用户名（需再次验证密码与两步验证码）。已登录的会话不受影响。
func (s *AuthService) ChangeUsername(ctx context.Context, actor Actor, req dto.ChangeUsernameRequest) (*dto.Admin, error) {
	name := strings.TrimSpace(req.Username)
	if !usernameRe.MatchString(name) {
		return nil, errcode.InvalidParams.WithMessage("用户名为 3–32 位字母、数字或 _ . -")
	}
	if err := s.Reauth(ctx, actor, req.Password, req.TOTPCode); err != nil {
		return nil, err
	}
	a, err := s.admins.Get(ctx, actor.AdminID)
	if err != nil {
		return nil, errcode.Unauthorized
	}
	if a.Username == name {
		return nil, errcode.InvalidParams.WithMessage("新用户名与当前相同")
	}
	if other, err := s.admins.GetByUsername(ctx, name); err == nil && other.ID != a.ID {
		return nil, errcode.Conflict.WithMessage("用户名 %s 已被使用", name)
	}
	if err := s.admins.Update(ctx, a.ID, map[string]any{"username": name, "updated_at": time.Now().UTC()}); err != nil {
		if repository.IsDuplicate(err) {
			return nil, errcode.Conflict.WithMessage("用户名 %s 已被使用", name)
		}
		return nil, err
	}
	s.audit.Record(ctx, actor, AuditUsernameChange, name, true, map[string]string{"from": a.Username, "to": name})
	return s.Me(ctx, a.ID)
}

// Reauth 敏感操作前再次验证密码与两步验证码；失败计入登录失败次数。
func (s *AuthService) Reauth(ctx context.Context, actor Actor, password, code string) error {
	a, err := s.admins.Get(ctx, actor.AdminID)
	if err != nil {
		return errcode.Unauthorized
	}
	if a.LockedUntil != nil && a.LockedUntil.After(time.Now().UTC()) {
		return errcode.AccountLocked
	}
	if !secure.VerifyPassword(password, a.PasswordHash) {
		s.fail(ctx, a, actor, "reauth: bad password")
		return errcode.BadCredentials.WithMessage("密码错误")
	}
	if a.TOTPEnabled {
		if code == "" {
			return errcode.TOTPRequired
		}
		if !s.checkTOTP(a, code) {
			s.fail(ctx, a, actor, "reauth: bad totp")
			return errcode.TOTPInvalid
		}
	}
	return nil
}

// ---- 两步验证 ----

// TOTPSetup 生成新密钥（未启用，需 TOTPEnable 用验证码确认）。
func (s *AuthService) TOTPSetup(ctx context.Context, actor Actor) (*dto.TOTPSetupResponse, error) {
	a, err := s.admins.Get(ctx, actor.AdminID)
	if err != nil {
		return nil, errcode.Unauthorized
	}
	if a.TOTPEnabled {
		return nil, errcode.Conflict.WithMessage("两步验证已启用")
	}
	key, err := totp.Generate(totp.GenerateOpts{Issuer: "ResidentKit", AccountName: a.Username})
	if err != nil {
		return nil, err
	}
	sealed, err := s.sealer.Seal(key.Secret())
	if err != nil {
		return nil, err
	}
	if err := s.admins.Update(ctx, a.ID, map[string]any{"totp_secret": sealed, "updated_at": time.Now().UTC()}); err != nil {
		return nil, err
	}
	return &dto.TOTPSetupResponse{Secret: key.Secret(), URL: key.URL()}, nil
}

func (s *AuthService) TOTPEnable(ctx context.Context, actor Actor, code string) error {
	a, err := s.admins.Get(ctx, actor.AdminID)
	if err != nil {
		return errcode.Unauthorized
	}
	if a.TOTPSecret == "" || !s.checkTOTP(a, code) {
		s.audit.Record(ctx, actor, AuditTOTPEnable, a.Username, false, nil)
		return errcode.TOTPInvalid
	}
	err = s.admins.Update(ctx, a.ID, map[string]any{"totp_enabled": true, "updated_at": time.Now().UTC()})
	if err == nil {
		s.audit.Record(ctx, actor, AuditTOTPEnable, a.Username, true, nil)
	}
	return err
}

func (s *AuthService) TOTPDisable(ctx context.Context, actor Actor, code string) error {
	a, err := s.admins.Get(ctx, actor.AdminID)
	if err != nil {
		return errcode.Unauthorized
	}
	if !a.TOTPEnabled || !s.checkTOTP(a, code) {
		s.audit.Record(ctx, actor, AuditTOTPDisable, a.Username, false, nil)
		return errcode.TOTPInvalid
	}
	err = s.admins.Update(ctx, a.ID, map[string]any{"totp_enabled": false, "totp_secret": "",
		"updated_at": time.Now().UTC()})
	if err == nil {
		s.audit.Record(ctx, actor, AuditTOTPDisable, a.Username, true, nil)
	}
	return err
}

func (s *AuthService) checkTOTP(a *model.Admin, code string) bool {
	secret, err := s.sealer.Open(a.TOTPSecret)
	return err == nil && totp.Validate(code, secret)
}

// ---- 命令行 ----

// CreateAdmin 由命令行调用，创建管理员。
func (s *AuthService) CreateAdmin(ctx context.Context, username, password string) error {
	if len(password) < minPasswordLen {
		return fmt.Errorf("密码至少 8 位")
	}
	if _, err := s.admins.GetByUsername(ctx, username); err == nil {
		return fmt.Errorf("用户 %s 已存在", username)
	}
	hash, err := secure.HashPassword(password)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	return s.admins.Create(ctx, &model.Admin{Username: username, PasswordHash: hash, Role: "admin",
		CreatedAt: now, UpdatedAt: now})
}

// ResetPassword 由命令行调用（忘记密码时），同时关闭两步验证并解锁。
func (s *AuthService) ResetPassword(ctx context.Context, username, password string) error {
	if len(password) < minPasswordLen {
		return fmt.Errorf("密码至少 8 位")
	}
	a, err := s.admins.GetByUsername(ctx, username)
	if err != nil {
		return fmt.Errorf("用户 %s 不存在", username)
	}
	hash, err := secure.HashPassword(password)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if err := s.admins.Update(ctx, a.ID, map[string]any{"password_hash": hash, "totp_enabled": false,
		"totp_secret": "", "failed_count": 0, "locked_until": nil, "updated_at": now}); err != nil {
		return err
	}
	return s.refresh.RevokeAllForAdmin(ctx, a.ID, now)
}
