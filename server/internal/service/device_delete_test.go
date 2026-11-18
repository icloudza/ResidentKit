package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/icloudza/ResidentKit/server/internal/model"
	"github.com/icloudza/ResidentKit/server/internal/pkg/errcode"
	"github.com/icloudza/ResidentKit/server/internal/repository"
)

// 删除设备的各步骤按顺序记在 log 里。
type delLog struct{ steps []string }

type delDevices struct {
	repository.DeviceRepository
	d   *model.Device
	log *delLog
}

func (f *delDevices) Get(context.Context, uint64) (*model.Device, error) { c := *f.d; return &c, nil }
func (f *delDevices) Delete(context.Context, uint64) error {
	f.log.steps = append(f.log.steps, "device")
	return nil
}

type delTokens struct {
	repository.DeviceTokenRepository
	log *delLog
}

func (f *delTokens) RevokeByDevice(context.Context, uint64, time.Time) (int64, error) {
	f.log.steps = append(f.log.steps, "tokens")
	return 1, nil
}

// 分批删除：第一次删掉 n 条，之后返回 0。
type delBatch struct {
	name string
	left int64
	log  *delLog
}

func (f *delBatch) del(context.Context, uint64) (int64, error) {
	n := min(f.left, 10000)
	f.left -= n
	f.log.steps = append(f.log.steps, f.name)
	return n, nil
}

type delEvents struct {
	repository.EventRepository
	*delBatch
}

func (f delEvents) DeleteByDevice(ctx context.Context, id uint64) (int64, error) {
	return f.del(ctx, id)
}

type delInvocations struct {
	repository.InvocationRepository
	*delBatch
}

func (f delInvocations) DeleteByDevice(ctx context.Context, id uint64) (int64, error) {
	return f.del(ctx, id)
}

type delUpdates struct {
	repository.DeviceUpdateRepository
	log *delLog
}

func (f delUpdates) DeleteByDevice(context.Context, uint64) error {
	f.log.steps = append(f.log.steps, "updates")
	return nil
}

type delConfigs struct {
	repository.DeviceConfigRepository
	log *delLog
}

func (f delConfigs) Delete(context.Context, uint64) error {
	f.log.steps = append(f.log.steps, "config")
	return nil
}

func deleteSetup(online bool) (*DeviceService, *delLog) {
	log := &delLog{}
	repo := &repository.Set{
		Devices:       &delDevices{d: &model.Device{ID: 7, Online: online}, log: log},
		DeviceTokens:  &delTokens{log: log},
		Invocations:   delInvocations{delBatch: &delBatch{name: "inv", left: 15000, log: log}},
		Events:        delEvents{delBatch: &delBatch{name: "ev", left: 3, log: log}},
		DeviceUpdates: delUpdates{log: log},
		DeviceConfigs: delConfigs{log: log},
		Audit:         fakeAudit{},
	}
	audit := NewAuditService(repo.Audit, zap.NewNop())
	return NewDeviceService(repo, audit, NewBus()), log
}

func TestDeleteDevice(t *testing.T) {
	svc, log := deleteSetup(false)
	require.NoError(t, svc.Delete(context.Background(), Actor{AdminID: 1}, 7))
	// 先吊销令牌（设备再连会被要求重新接入），再删配置、更新记录和设备本身，最后分批清理历史。
	require.Equal(t, []string{"tokens", "config", "updates", "device", "inv", "inv", "inv", "ev", "ev"}, log.steps)
}

func TestDeleteOnlineDeviceRefused(t *testing.T) {
	svc, log := deleteSetup(true)
	err := svc.Delete(context.Background(), Actor{AdminID: 1}, 7)
	require.Equal(t, errcode.Conflict.Code, errcode.From(err).Code)
	require.Empty(t, log.steps)
}
