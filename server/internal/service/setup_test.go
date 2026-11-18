package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/icloudza/ResidentKit/server/internal/dto"
	"github.com/icloudza/ResidentKit/server/internal/model"
	"github.com/icloudza/ResidentKit/server/internal/pkg/errcode"
	"github.com/icloudza/ResidentKit/server/internal/repository"
)

type fakeAdmins struct {
	repository.AdminRepository
	list []model.Admin
}

func (f *fakeAdmins) Count(context.Context) (int64, error) { return int64(len(f.list)), nil }
func (f *fakeAdmins) Create(_ context.Context, a *model.Admin) error {
	a.ID = uint64(len(f.list) + 1)
	f.list = append(f.list, *a)
	return nil
}

type fakeRefresh struct {
	repository.RefreshTokenRepository
}

func (fakeRefresh) Create(context.Context, *model.RefreshToken) error { return nil }

func setupSvc(t *testing.T) (*SetupService, *fakeAdmins) {
	admins := &fakeAdmins{}
	repo := &repository.Set{Admins: admins, RefreshTokens: fakeRefresh{}, Audit: fakeAudit{}}
	audit := NewAuditService(repo.Audit, zap.NewNop())
	auth := NewAuthService(repo, audit, nil, AuthOptions{JWTKey: []byte("k"), AccessTTL: time.Minute, RefreshTTL: time.Hour})
	s := NewSetupService(repo, auth, audit, zap.NewNop())
	require.NoError(t, s.Prepare(context.Background()))
	return s, admins
}

func TestSetupFlow(t *testing.T) {
	s, admins := setupSvc(t)
	require.True(t, s.NeedsSetup())
	code := s.code

	_, err := s.Complete(context.Background(), dto.SetupRequest{Code: "WRONG-CODE-0000", Username: "admin", Password: "12345678"}, "", "")
	require.ErrorIs(t, err, errcode.InvalidParams)
	require.Empty(t, admins.list)

	_, err = s.Complete(context.Background(), dto.SetupRequest{Code: code, Username: "admin", Password: "short"}, "", "")
	require.Error(t, err)

	// 不区分大小写、可省略分隔符。
	sess, err := s.Complete(context.Background(), dto.SetupRequest{Code: lower(code), Username: "admin", Password: "12345678"}, "", "")
	require.NoError(t, err)
	require.NotEmpty(t, sess.AccessToken)
	require.Len(t, admins.list, 1)
	require.False(t, s.NeedsSetup())

	// 初始化码只能用一次。
	_, err = s.Complete(context.Background(), dto.SetupRequest{Code: code, Username: "evil", Password: "12345678"}, "", "")
	require.Error(t, err)
	require.Len(t, admins.list, 1)
}

func TestSetupSkippedWhenAdminExists(t *testing.T) {
	admins := &fakeAdmins{list: []model.Admin{{ID: 1}}}
	repo := &repository.Set{Admins: admins, Audit: fakeAudit{}}
	s := NewSetupService(repo, nil, NewAuditService(repo.Audit, zap.NewNop()), zap.NewNop())
	require.NoError(t, s.Prepare(context.Background()))
	require.False(t, s.NeedsSetup())
}

func lower(s string) string {
	out := []byte{}
	for i := 0; i < len(s); i++ {
		if c := s[i]; c != '-' {
			if c >= 'A' && c <= 'Z' {
				c += 'a' - 'A'
			}
			out = append(out, c)
		}
	}
	return string(out)
}
