package service

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/icloudza/ResidentKit/server/internal/dto"
	"github.com/icloudza/ResidentKit/server/internal/model"
	"github.com/icloudza/ResidentKit/server/internal/pkg/errcode"
	"github.com/icloudza/ResidentKit/server/internal/repository"
)

// ---- 内存假实现 ----

type fakeDevices struct {
	repository.DeviceRepository
	d *model.Device
}

func (f *fakeDevices) Get(context.Context, uint64) (*model.Device, error) { c := *f.d; return &c, nil }

type fakeAudit struct{ repository.AuditRepository }

func (fakeAudit) Insert(context.Context, *model.AuditLog) error { return nil }

type fakeInvocations struct {
	repository.InvocationRepository
	mu   sync.Mutex
	rows map[uint64]*model.Invocation
	next uint64
}

func (f *fakeInvocations) Create(_ context.Context, inv *model.Invocation) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	// 与 MySQL 唯一索引 uk_invocations_idem 一致：同一密钥同一幂等键只能有一条。
	if inv.APIKeyID != nil && inv.IdemKey != nil {
		for _, r := range f.rows {
			if r.APIKeyID != nil && r.IdemKey != nil && *r.APIKeyID == *inv.APIKeyID && *r.IdemKey == *inv.IdemKey {
				return &mysql.MySQLError{Number: 1062, Message: "Duplicate entry"}
			}
		}
	}
	f.next++
	inv.ID = f.next
	c := *inv
	f.rows[inv.ID] = &c
	return nil
}

func (f *fakeInvocations) Get(_ context.Context, id uint64) (*model.Invocation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := *f.rows[id]
	return &c, nil
}

func (f *fakeInvocations) GetByUID(_ context.Context, uid string) (*model.Invocation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows {
		if r.UID == uid {
			c := *r
			return &c, nil
		}
	}
	return nil, repository.ErrNotFound
}

func (f *fakeInvocations) Transition(_ context.Context, id uint64, from []string, u map[string]any) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := f.rows[id]
	if !slices.Contains(from, r.Status) {
		return false, nil
	}
	if s, ok := u["status"].(string); ok {
		r.Status = s
	}
	if b, ok := u["result"].([]byte); ok {
		r.Result = b
	}
	return true, nil
}

func (f *fakeInvocations) GetByIdem(_ context.Context, key uint64, idem string) (*model.Invocation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows {
		if r.APIKeyID != nil && r.IdemKey != nil && *r.APIKeyID == key && *r.IdemKey == idem {
			c := *r
			return &c, nil
		}
	}
	return nil, repository.ErrNotFound
}

func (f *fakeInvocations) PendingForDevice(_ context.Context, dev uint64, now time.Time) ([]model.Invocation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []model.Invocation
	for _, r := range f.rows {
		if r.DeviceID == dev && r.Status == model.InvPending && r.ExpiresAt.After(now) {
			out = append(out, *r)
		}
	}
	slices.SortFunc(out, func(a, b model.Invocation) int { return int(a.ID) - int(b.ID) }) // 与 ORDER BY id 一致
	return out, nil
}

func (f *fakeInvocations) count(status string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, r := range f.rows {
		if r.Status == status {
			n++
		}
	}
	return n
}

func (f *fakeInvocations) SentForDevice(_ context.Context, dev uint64) ([]model.Invocation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []model.Invocation
	for _, r := range f.rows {
		if r.DeviceID == dev && r.Status == model.InvSent {
			out = append(out, *r)
		}
	}
	return out, nil
}

type fakeSender struct {
	mu     sync.Mutex
	online bool
	sent   []string
}

func (s *fakeSender) SendInvoke(_ uint64, uid, _, _ string, _ json.RawMessage) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.online {
		s.sent = append(s.sent, uid)
	}
	return s.online
}

func (s *fakeSender) list() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.sent)
}

func setup(online bool) (*InvocationService, *fakeInvocations, *fakeSender) {
	cmds, _ := json.Marshal(map[string][]string{"example.heartbeat": {"status"}, "system.update": {"install"}})
	dev := &model.Device{ID: 1, Online: online, RemoteCommands: cmds}
	repo := &repository.Set{Devices: &fakeDevices{d: dev}, Audit: fakeAudit{},
		Invocations: &fakeInvocations{rows: map[uint64]*model.Invocation{}}}
	bus := NewBus()
	audit := NewAuditService(repo.Audit, zap.NewNop())
	devices := NewDeviceService(repo, audit, bus)
	svc := NewInvocationService(repo, devices, audit, bus, zap.NewNop(), 10*time.Minute, 5*time.Second)
	sender := &fakeSender{online: online}
	svc.SetSender(sender)
	return svc, repo.Invocations.(*fakeInvocations), sender
}

var req = dto.InvokeRequest{Module: "example.heartbeat", Command: "status"}

func TestInvokeOnlineAndResult(t *testing.T) {
	svc, repo, sender := setup(true)
	inv, err := svc.Create(context.Background(), Actor{AdminID: 1}, 1, req)
	require.NoError(t, err)
	require.Equal(t, model.InvSent, inv.Status)
	require.Len(t, sender.sent, 1)

	svc.OnResult(context.Background(), 1, InvokeResult{UID: sender.sent[0], Payload: json.RawMessage(`{"beats":3}`)})
	got, _ := repo.Get(context.Background(), inv.ID)
	require.Equal(t, model.InvSucceeded, got.Status)
	require.JSONEq(t, `{"beats":3}`, string(got.Result))

	// 其他设备伪造的结果不被接受。
	inv2, _ := svc.Create(context.Background(), Actor{}, 1, req)
	svc.OnResult(context.Background(), 2, InvokeResult{UID: sender.sent[1]})
	got, _ = repo.Get(context.Background(), inv2.ID)
	require.Equal(t, model.InvSent, got.Status)
}

func TestInvokeOfflineQueuedThenDelivered(t *testing.T) {
	svc, repo, sender := setup(false)
	inv, err := svc.Create(context.Background(), Actor{}, 1, req)
	require.NoError(t, err)
	require.Equal(t, model.InvPending, inv.Status)

	sender.mu.Lock()
	sender.online = true
	sender.mu.Unlock()
	svc.DeliverPending(context.Background(), 1)
	got, _ := repo.Get(context.Background(), inv.ID)
	require.Equal(t, model.InvSent, got.Status)

	// 下发后断线：标记为 lost，不再重发。
	svc.OnDisconnect(context.Background(), 1)
	got, _ = repo.Get(context.Background(), inv.ID)
	require.Equal(t, model.InvLost, got.Status)
	svc.DeliverPending(context.Background(), 1)
	require.Len(t, sender.sent, 1)
}

func TestInvokeRejectsUndeclaredCommand(t *testing.T) {
	svc, _, _ := setup(true)
	_, err := svc.Create(context.Background(), Actor{}, 1, dto.InvokeRequest{Module: "net.link", Command: "configure"})
	require.ErrorIs(t, err, errcode.CommandNotPermitted)
}

func TestInvokeOfflineNoQueue(t *testing.T) {
	svc, _, _ := setup(false)
	r := req
	r.TTLSeconds = -1
	_, err := svc.Create(context.Background(), Actor{}, 1, r)
	require.ErrorIs(t, err, errcode.DeviceOffline)
}

func TestInvokeWaitReturnsResult(t *testing.T) {
	svc, _, sender := setup(true)
	r := req
	r.WaitSeconds = 3
	go func() {
		for len(sender.list()) == 0 {
			time.Sleep(10 * time.Millisecond)
		}
		svc.OnResult(context.Background(), 1, InvokeResult{UID: sender.list()[0], Payload: json.RawMessage(`{}`)})
	}()
	inv, err := svc.Create(context.Background(), Actor{}, 1, r)
	require.NoError(t, err)
	require.Equal(t, model.InvSucceeded, inv.Status)
}

// 并发提交大量指令：同一设备在途不超过上限，其余排队；每条结果返回后补发，最终全部恰好下发一次。
func TestConcurrentInvokesRespectInflightLimit(t *testing.T) {
	svc, repo, sender := setup(true)
	const n = 100
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.CreateWith(context.Background(), Actor{Username: "api:test"}, 1, req, CreateOptions{APIKeyID: 7})
			require.NoError(t, err)
		}()
	}
	wg.Wait()
	require.Equal(t, MaxInflightPerDevice, len(sender.list()), "首轮只下发上限条数")
	require.Equal(t, n-MaxInflightPerDevice, repo.count(model.InvPending))

	// 模拟设备依次返回结果：每返回一条就补发下一条。
	done := 0
	deadline := time.Now().Add(5 * time.Second)
	for done < n && time.Now().Before(deadline) {
		sent := sender.list()
		if done >= len(sent) {
			time.Sleep(5 * time.Millisecond)
			continue
		}
		svc.OnResult(context.Background(), 1, InvokeResult{UID: sent[done], Payload: json.RawMessage(`{}`)})
		done++
		require.LessOrEqual(t, repo.count(model.InvSent), MaxInflightPerDevice)
	}
	require.Equal(t, n, done)
	require.Equal(t, n, repo.count(model.InvSucceeded))
	uids := sender.list()
	require.Len(t, uids, n)
	require.Len(t, slices.Compact(slices.Sorted(slices.Values(uids))), n, "没有重复下发")
}

// 相同幂等键并发提交：只建一条指令，所有请求拿到同一个 ID。
func TestConcurrentSameIdempotencyKey(t *testing.T) {
	svc, repo, sender := setup(true)
	const n = 50
	ids := make(chan uint64, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			inv, err := svc.CreateWith(context.Background(), Actor{}, 1, req, CreateOptions{APIKeyID: 7, IdemKey: "order-42"})
			require.NoError(t, err)
			ids <- inv.ID
		}()
	}
	wg.Wait()
	close(ids)
	first := uint64(0)
	for id := range ids {
		if first == 0 {
			first = id
		}
		require.Equal(t, first, id)
	}
	repo.mu.Lock()
	require.Len(t, repo.rows, 1)
	repo.mu.Unlock()
	require.Len(t, sender.list(), 1, "只下发一次")

	// 同一个键用于不同的命令：冲突。
	_, err := svc.CreateWith(context.Background(), Actor{}, 1, dto.InvokeRequest{Module: "system.update", Command: "install"},
		CreateOptions{APIKeyID: 7, IdemKey: "order-42"})
	require.ErrorIs(t, err, errcode.Conflict)
}

func TestOpenAPIDeniesInternalCommands(t *testing.T) {
	svc, _, _ := setup(true)
	_, err := svc.CreateWith(context.Background(), Actor{}, 1, dto.InvokeRequest{Module: "system.update", Command: "install"},
		CreateOptions{APIKeyID: 7, DenyInternal: true})
	require.ErrorIs(t, err, errcode.Forbidden)
}
