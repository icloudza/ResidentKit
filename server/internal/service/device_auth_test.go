package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/icloudza/ResidentKit/server/internal/model"
	"github.com/icloudza/ResidentKit/server/internal/repository"
)

type authTokens struct {
	repository.DeviceTokenRepository
	active, any bool
}

func (f authTokens) FindActiveByHash(context.Context, string) (*model.DeviceToken, error) {
	if f.active {
		return &model.DeviceToken{}, nil
	}
	return nil, repository.ErrNotFound
}

func (f authTokens) FindByHash(context.Context, string) (*model.DeviceToken, error) {
	if f.any {
		return &model.DeviceToken{}, nil
	}
	return nil, repository.ErrNotFound
}

func TestAuthenticateTokenStates(t *testing.T) {
	ctx := context.Background()
	dev := "rkd_" + "0123456789abcdefghijklmnopqrstuv"
	cases := []struct {
		name   string
		tokens authTokens
		token  string
		want   error
	}{
		{"有效", authTokens{active: true, any: true}, dev, nil},
		{"已吊销", authTokens{any: true}, dev, ErrTokenRevoked},
		{"本服务器没有（换了服务器）", authTokens{}, dev, ErrTokenUnknown},
		{"不是设备令牌", authTokens{}, "xyz_0123456789abcdefghijklmnop", ErrBadDeviceToken},
		{"太短", authTokens{}, "rkd_short", ErrBadDeviceToken},
	}
	for _, c := range cases {
		s := &DeviceService{tokens: c.tokens}
		_, err := s.Authenticate(ctx, c.token)
		if c.want == nil {
			require.NoError(t, err, c.name)
		} else {
			require.ErrorIs(t, err, c.want, c.name)
		}
	}
}
