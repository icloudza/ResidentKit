package service

import (
	"context"
	"encoding/json"
	"time"

	"go.uber.org/zap"

	"github.com/icloudza/ResidentKit/server/internal/dto"
	"github.com/icloudza/ResidentKit/server/internal/model"
	"github.com/icloudza/ResidentKit/server/internal/repository"
)

// 审计动作。
const (
	AuditSetup          = "auth.setup"
	AuditLogin          = "auth.login"
	AuditLogout         = "auth.logout"
	AuditPasswordChange = "auth.password"
	AuditUsernameChange = "auth.username"
	AuditTOTPEnable     = "auth.totp.enable"
	AuditTOTPDisable    = "auth.totp.disable"
	AuditInvoke         = "device.invoke"
	AuditDeviceAlias    = "device.alias"
	AuditDeviceDelete   = "device.delete"
	AuditTokenIssue     = "token.issue"
	AuditTokenRevoke    = "token.revoke"
	AuditTokenDelete    = "token.delete"
	AuditReleaseUpload  = "release.upload"
	AuditReleaseRevoke  = "release.withdraw"
	AuditRolloutCreate  = "rollout.create"
	AuditRolloutChange  = "rollout.change"
	AuditUpdateRetry    = "update.retry"
	AuditDeviceConfig   = "device.config"
	AuditEnrollApprove  = "enroll.approve"
	AuditEnrollReject   = "enroll.reject"
	AuditAPIKeyCreate   = "apikey.create"
	AuditAPIKeyRevoke   = "apikey.revoke"
	AuditAPIKeyDelete   = "apikey.delete"
	AuditAPIKeyUpdate   = "apikey.update"
	AuditRepoLinkCreate = "repo.link.create"
	AuditRepoLinkRevoke = "repo.link.revoke"
	AuditTicketCreate   = "enroll.ticket.create"
	AuditTicketRevoke   = "enroll.ticket.revoke"
	AuditReleaseRepo    = "release.repo"
	AuditRepoLinkUpdate = "repo.link.update"
	AuditRepoLinkDelete = "repo.link.delete"
)

// Actor 发起操作的管理员及来源。
type Actor struct {
	AdminID  uint64
	Username string
	IP       string
}

type AuditService struct {
	repo repository.AuditRepository
	log  *zap.Logger
}

func NewAuditService(repo repository.AuditRepository, log *zap.Logger) *AuditService {
	return &AuditService{repo: repo, log: log}
}

// Record 写审计日志。失败只记日志，不影响业务。
func (s *AuditService) Record(ctx context.Context, a Actor, action, target string, success bool, detail any) {
	entry := &model.AuditLog{
		Username: a.Username, Action: action, Target: target, IP: a.IP,
		Success: success, CreatedAt: time.Now().UTC(),
	}
	if a.AdminID != 0 {
		id := a.AdminID
		entry.AdminID = &id
	}
	if detail != nil {
		if b, err := json.Marshal(detail); err == nil {
			entry.Detail = b
		}
	}
	if err := s.repo.Insert(ctx, entry); err != nil {
		s.log.Error("audit insert failed", zap.String("action", action), zap.Error(err))
	}
}

func (s *AuditService) List(ctx context.Context, q repository.ListQuery) (dto.Page[dto.AuditLog], error) {
	rows, total, err := s.repo.List(ctx, q)
	if err != nil {
		return dto.Page[dto.AuditLog]{}, err
	}
	out := make([]dto.AuditLog, len(rows))
	for i := range rows {
		out[i] = dto.AuditLogFrom(&rows[i])
	}
	return dto.Page[dto.AuditLog]{Items: out, Total: total, Page: q.Num(), PageSize: q.Size()}, nil
}
