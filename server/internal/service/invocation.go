package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/icloudza/ResidentKit/server/internal/dto"
	"github.com/icloudza/ResidentKit/server/internal/model"
	"github.com/icloudza/ResidentKit/server/internal/pkg/errcode"
	"github.com/icloudza/ResidentKit/server/internal/repository"
)

// Sender 由设备网关实现，用于向在线设备下发指令。
type Sender interface {
	// SendInvoke 发送成功返回 true；设备不在线返回 false。
	SendInvoke(deviceID uint64, uid, module, command string, args json.RawMessage) bool
}

// InvokeResult 设备返回的 result 消息。
type InvokeResult struct {
	UID          string
	Payload      json.RawMessage
	ErrorCode    int
	ErrorMessage string
	HasError     bool
}

type InvocationService struct {
	repo       repository.InvocationRepository
	devices    *DeviceService
	audit      *AuditService
	bus        *Bus
	log        *zap.Logger
	defaultTTL time.Duration
	waitMax    time.Duration

	sender Sender

	mu      sync.Mutex
	waiters map[uint64][]chan struct{} // 同步等待结果的请求

	// 每台设备同时「已下发未返回」的指令上限。并发提交时其余的留在 pending 排队，
	// 有结果返回就补发下一条，避免一次性把大量指令塞进设备连接的发送队列。
	slotMu   sync.Mutex
	inflight map[uint64]map[uint64]struct{} // 设备 → 在途指令 ID
	// queued 设备可能有排队中的指令（名额满或离线时留在 pending）。没有就不必查库补发。
	queued map[uint64]bool
	// pumping 补发合并：正在补发时再来的请求只记一次「再跑一轮」，不并发查库。
	pumping map[uint64]int // 0 空闲 / 1 运行中 / 2 运行中且需再跑一轮
}

// MaxInflightPerDevice 每台设备同时在途的指令数上限。
const MaxInflightPerDevice = 16

// internalCommands 由系统自动调用的命令（配置下发、OTA），不对开放 API 开放。
var internalCommands = map[string]bool{"system.config/apply": true, "system.update/install": true}

// CreateOptions 经开放 API 创建指令时的附加信息。
type CreateOptions struct {
	APIKeyID     uint64 // 0 表示控制台
	IdemKey      string // 幂等键：同一密钥下相同的键只执行一次
	DenyInternal bool   // 拒绝系统内部命令
}

func NewInvocationService(r *repository.Set, devices *DeviceService, audit *AuditService, bus *Bus,
	log *zap.Logger, defaultTTL, waitMax time.Duration) *InvocationService {
	return &InvocationService{repo: r.Invocations, devices: devices, audit: audit, bus: bus, log: log,
		defaultTTL: defaultTTL, waitMax: waitMax, waiters: make(map[uint64][]chan struct{}),
		inflight: make(map[uint64]map[uint64]struct{}), queued: make(map[uint64]bool), pumping: make(map[uint64]int)}
}

// SetSender 注入网关（网关依赖本服务，故装配后再设置）。
func (s *InvocationService) SetSender(sender Sender) { s.sender = sender }

// Create 新建指令（控制台）：设备在线立即下发，离线则排队至 TTL 到期；可同步等待结果。
func (s *InvocationService) Create(ctx context.Context, a Actor, deviceID uint64, req dto.InvokeRequest) (*dto.Invocation, error) {
	return s.CreateWith(ctx, a, deviceID, req, CreateOptions{})
}

// CreateWith 新建指令，可附带开放 API 的密钥与幂等键。并发安全：幂等键由数据库唯一索引保证只建一条。
func (s *InvocationService) CreateWith(ctx context.Context, a Actor, deviceID uint64, req dto.InvokeRequest, opt CreateOptions) (*dto.Invocation, error) {
	if opt.APIKeyID != 0 && opt.IdemKey != "" {
		// 先查：重试请求直接拿到上次的指令，不再走创建流程。
		if prev, err := s.repo.GetByIdem(ctx, opt.APIKeyID, opt.IdemKey); err == nil {
			return s.replay(ctx, prev, deviceID, req)
		}
	}
	if opt.DenyInternal && internalCommands[req.Module+"/"+req.Command] {
		return nil, errcode.Forbidden.WithMessage("该命令不对外开放")
	}
	d, err := s.devices.Get(ctx, deviceID)
	if err != nil {
		return nil, err
	}
	// 只允许设备声明开放的远程命令（设备端也会再校验一次）。
	if !slices.Contains(dto.DeviceFrom(d).RemoteCommands[req.Module], req.Command) {
		if a.AdminID != 0 {
			s.audit.Record(ctx, a, AuditInvoke, fmt.Sprint(deviceID), false,
				map[string]string{"module": req.Module, "command": req.Command, "reason": "not permitted"})
		}
		return nil, errcode.CommandNotPermitted
	}
	ttl := s.defaultTTL
	switch {
	case req.TTLSeconds > 0:
		ttl = time.Duration(req.TTLSeconds) * time.Second
	case req.TTLSeconds < 0:
		ttl = 0
	}
	if ttl == 0 && !d.Online {
		return nil, errcode.DeviceOffline
	}

	args, _ := json.Marshal(req.Arguments)
	if req.Arguments == nil {
		args = []byte("{}")
	}
	now := time.Now().UTC()
	inv := &model.Invocation{
		UID: newUID(), DeviceID: deviceID, Module: req.Module, Command: req.Command, Arguments: args,
		Status: model.InvPending, ExpiresAt: now.Add(max(ttl, time.Minute)), CreatedAt: now, UpdatedAt: now,
		SourceIP: trunc(a.IP, 45),
	}
	if a.AdminID != 0 {
		id := a.AdminID
		inv.OperatorID = &id
	}
	if opt.APIKeyID != 0 {
		kid := opt.APIKeyID
		inv.APIKeyID = &kid
		if opt.IdemKey != "" {
			k := opt.IdemKey
			inv.IdemKey = &k
		}
	}
	if err := s.repo.Create(ctx, inv); err != nil {
		// 并发的相同幂等键：只有一个插入成功，其余返回已建的那条。
		if repository.IsDuplicate(err) && inv.IdemKey != nil {
			if prev, gerr := s.repo.GetByIdem(ctx, opt.APIKeyID, opt.IdemKey); gerr == nil {
				return s.replay(ctx, prev, deviceID, req)
			}
		}
		return nil, err
	}
	// 只审计控制台上人的操作。开放 API、OTA、配置下发是自动调用，量大且指令记录本身
	// 已含发起方（密钥、来源 IP）、参数与结果，再写审计只会淹没人工操作。
	if a.AdminID != 0 {
		s.audit.Record(ctx, a, AuditInvoke, fmt.Sprint(deviceID), true,
			map[string]any{"invocation": inv.ID, "module": req.Module, "command": req.Command})
	}

	s.deliver(ctx, inv, CommandTimeout(d, req.Module, req.Command))

	if req.WaitSeconds > 0 {
		// 等待超时不算失败：指令仍在进行，客户端可稍后查询。
		s.wait(ctx, inv.ID, min(time.Duration(req.WaitSeconds)*time.Second, s.waitMax))
	}
	cur, err := s.repo.Get(ctx, inv.ID)
	if err != nil {
		return nil, err
	}
	out := dto.InvocationFrom(cur)
	return &out, nil
}

// replay 幂等键命中：参数一致则返回原指令（可同步等待），否则冲突。
func (s *InvocationService) replay(ctx context.Context, prev *model.Invocation, deviceID uint64, req dto.InvokeRequest) (*dto.Invocation, error) {
	if prev.DeviceID != deviceID || prev.Module != req.Module || prev.Command != req.Command ||
		!sameArgs(prev.Arguments, req.Arguments) {
		return nil, errcode.Conflict.WithMessage("幂等键已被使用")
	}
	if req.WaitSeconds > 0 && !prev.Finished() {
		s.wait(ctx, prev.ID, min(time.Duration(req.WaitSeconds)*time.Second, s.waitMax))
	}
	cur, err := s.repo.Get(ctx, prev.ID)
	if err != nil {
		return nil, err
	}
	out := dto.InvocationFrom(cur)
	return &out, nil
}

// sameArgs 比较已存参数与新请求参数（经 JSON 规范化，忽略键顺序与数字表示差异）。
func sameArgs(stored []byte, args map[string]any) bool {
	var a, b any
	if len(stored) == 0 {
		stored = []byte("{}")
	}
	if json.Unmarshal(stored, &a) != nil {
		return false
	}
	nb, _ := json.Marshal(args)
	if args == nil {
		nb = []byte("{}")
	}
	if json.Unmarshal(nb, &b) != nil {
		return false
	}
	return reflect.DeepEqual(a, b)
}

// ---- 在途上限 ----

// acquireSlot 为一条指令占一个在途名额；已满或该指令已在途时返回 false。
func (s *InvocationService) acquireSlot(deviceID, invID uint64) bool {
	s.slotMu.Lock()
	defer s.slotMu.Unlock()
	set := s.inflight[deviceID]
	if set == nil {
		set = map[uint64]struct{}{}
		s.inflight[deviceID] = set
	}
	// 已在途（另一路正在下发或已下发）：不重复占用。否则后来者状态迁移失败时会把对方的名额释放掉。
	if _, ok := set[invID]; ok {
		return false
	}
	if len(set) >= MaxInflightPerDevice {
		return false
	}
	set[invID] = struct{}{}
	return true
}

// releaseSlot 释放在途名额，返回是否确实占用过（重复释放无副作用）。
func (s *InvocationService) releaseSlot(deviceID, invID uint64) bool {
	s.slotMu.Lock()
	defer s.slotMu.Unlock()
	set := s.inflight[deviceID]
	if _, ok := set[invID]; !ok {
		return false
	}
	delete(set, invID)
	if len(set) == 0 {
		delete(s.inflight, deviceID)
	}
	return true
}

func (s *InvocationService) slotsFull(deviceID uint64) bool {
	s.slotMu.Lock()
	defer s.slotMu.Unlock()
	return len(s.inflight[deviceID]) >= MaxInflightPerDevice
}

func (s *InvocationService) markQueued(deviceID uint64) {
	s.slotMu.Lock()
	s.queued[deviceID] = true
	s.slotMu.Unlock()
}

// pump 有名额空出后补发排队中的指令。高并发时每条结果都会触发：没有排队的指令就不查库，
// 正在补发时只记一次「再跑一轮」，同一设备同时只有一个补发在查库。
func (s *InvocationService) pump(deviceID uint64) {
	s.slotMu.Lock()
	if !s.queued[deviceID] {
		s.slotMu.Unlock()
		return
	}
	if s.pumping[deviceID] > 0 {
		s.pumping[deviceID] = 2
		s.slotMu.Unlock()
		return
	}
	s.pumping[deviceID] = 1
	s.slotMu.Unlock()
	go func() {
		for {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			s.DeliverPending(ctx, deviceID)
			cancel()
			s.slotMu.Lock()
			if s.pumping[deviceID] == 2 && s.queued[deviceID] && len(s.inflight[deviceID]) < MaxInflightPerDevice {
				s.pumping[deviceID] = 1
				s.slotMu.Unlock()
				continue
			}
			delete(s.pumping, deviceID)
			s.slotMu.Unlock()
			return
		}
	}()
}

// deliver 尝试下发一条 pending 指令；timeout 为下发后等待结果的上限。
func (s *InvocationService) deliver(ctx context.Context, inv *model.Invocation, timeout time.Duration) {
	if s.sender == nil {
		return
	}
	// 在途已满：留在 pending 排队，有结果返回后由 pump 补发。
	if !s.acquireSlot(inv.DeviceID, inv.ID) {
		s.markQueued(inv.DeviceID)
		return
	}
	now := time.Now().UTC()
	// 先置为 sent 再发送：结果可能比本函数返回还早到达。expires_at 改为结果期限，超过仍无结果记为 lost。
	ok, err := s.repo.Transition(ctx, inv.ID, []string{model.InvPending},
		map[string]any{"status": model.InvSent, "sent_at": now, "updated_at": now, "expires_at": now.Add(timeout)})
	if err != nil || !ok {
		s.releaseSlot(inv.DeviceID, inv.ID)
		return
	}
	if s.sender.SendInvoke(inv.DeviceID, inv.UID, inv.Module, inv.Command, json.RawMessage(inv.Arguments)) {
		s.publish(ctx, inv.ID)
		return
	}
	// 设备不在线：退回 pending（恢复排队期限），等它上线。
	_, _ = s.repo.Transition(ctx, inv.ID, []string{model.InvSent},
		map[string]any{"status": model.InvPending, "sent_at": nil, "expires_at": inv.ExpiresAt, "updated_at": time.Now().UTC()})
	s.releaseSlot(inv.DeviceID, inv.ID)
	s.markQueued(inv.DeviceID)
}

// DeliverPending 设备上线或有名额空出时补发排队中的指令。
func (s *InvocationService) DeliverPending(ctx context.Context, deviceID uint64) {
	// 先清标记再查库：查询之后新排队的指令会重新置位，不会漏补。
	s.slotMu.Lock()
	delete(s.queued, deviceID)
	s.slotMu.Unlock()
	list, err := s.repo.PendingForDevice(ctx, deviceID, time.Now().UTC())
	if err != nil {
		s.markQueued(deviceID)
		s.log.Error("load pending invocations", zap.Error(err))
		return
	}
	if len(list) == 0 {
		return
	}
	d, err := s.devices.Get(ctx, deviceID)
	if err != nil {
		d = nil // 取不到命令说明就用默认期限
	}
	for i := range list {
		if s.slotsFull(deviceID) {
			s.markQueued(deviceID)
			return
		}
		s.deliver(ctx, &list[i], CommandTimeout(d, list[i].Module, list[i].Command))
	}
	if len(list) == pendingBatch { // 一批没取完
		s.markQueued(deviceID)
	}
}

// OnResult 处理设备返回的结果。
func (s *InvocationService) OnResult(ctx context.Context, deviceID uint64, r InvokeResult) {
	inv, err := s.repo.GetByUID(ctx, r.UID)
	if err != nil || inv.DeviceID != deviceID { // 只接受指令目标设备的结果
		return
	}
	now := time.Now().UTC()
	updates := map[string]any{"finished_at": now, "updated_at": now}
	if r.HasError {
		updates["status"] = model.InvFailed
		updates["error_code"] = r.ErrorCode
		updates["error_message"] = trunc(r.ErrorMessage, 255)
	} else {
		updates["status"] = model.InvSucceeded
		if len(r.Payload) > 0 {
			updates["result"] = []byte(r.Payload)
		}
	}
	// lost 也接受：连接断开后结果偶尔仍会随重连到达。
	if ok, _ := s.repo.Transition(ctx, inv.ID, []string{model.InvSent, model.InvLost}, updates); ok {
		// 设备信息的结果顺带写回设备（先于通知，同步等待的请求拿到结果时设备已是最新）。
		if !r.HasError && inv.Module == "net.link" && inv.Command == "info" {
			s.devices.ApplyInfoResult(ctx, deviceID, r.Payload)
		}
		s.publish(ctx, inv.ID)
		s.notify(inv.ID)
	}
	if s.releaseSlot(deviceID, inv.ID) {
		s.pump(deviceID)
	}
}

// OnDisconnect 连接断开：已下发未返回的指令标记为 lost（不自动重发，避免重复执行）。
func (s *InvocationService) OnDisconnect(ctx context.Context, deviceID uint64) {
	s.slotMu.Lock()
	delete(s.inflight, deviceID)
	s.slotMu.Unlock()
	list, err := s.repo.SentForDevice(ctx, deviceID)
	if err != nil {
		return
	}
	now := time.Now().UTC()
	for _, inv := range list {
		ok, _ := s.repo.Transition(ctx, inv.ID, []string{model.InvSent}, map[string]any{
			"status": model.InvLost, "finished_at": now, "updated_at": now, "error_message": "连接断开，结果未知"})
		if ok {
			s.publish(ctx, inv.ID)
			s.notify(inv.ID)
		}
	}
}

// Recover 服务启动时处理上一进程遗留的 sent 指令。
func (s *InvocationService) Recover(ctx context.Context) error {
	_, err := s.repo.MarkAllSentLost(ctx, time.Now().UTC())
	return err
}

// sentTimeout 命令未声明 timeout 时，下发后等待结果的上限；超过视为结果未知并释放在途名额。
const sentTimeout = 2 * time.Minute

// pendingBatch 每次补发最多取的排队指令数（与 PendingForDevice 的 LIMIT 一致）。
const pendingBatch = 100

// ExpirePending 定时任务：过期未下发的指令；过了结果期限仍无结果的标记为 lost。
func (s *InvocationService) ExpirePending(ctx context.Context) {
	now := time.Now().UTC()
	if n, err := s.repo.ExpirePending(ctx, now); err == nil && n > 0 {
		s.log.Info("invocations expired", zap.Int64("count", n))
	}
	stale, err := s.repo.SentOverdue(ctx, now)
	if err != nil {
		return
	}
	for _, inv := range stale {
		ok, _ := s.repo.Transition(ctx, inv.ID, []string{model.InvSent}, map[string]any{
			"status": model.InvLost, "finished_at": now, "updated_at": now, "error_message": "设备超时未返回结果"})
		if ok {
			s.publish(ctx, inv.ID)
			s.notify(inv.ID)
		}
		if s.releaseSlot(inv.DeviceID, inv.ID) {
			s.pump(inv.DeviceID)
		}
	}
}

func (s *InvocationService) Get(ctx context.Context, id uint64) (*dto.Invocation, error) {
	inv, err := s.repo.Get(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, errcode.NotFound.WithMessage("指令不存在")
	}
	if err != nil {
		return nil, err
	}
	out := dto.InvocationFrom(inv)
	return &out, nil
}

// GetForKey 开放 API：只返回该密钥发起的指令（其他一律 404）；wait > 0 时在完成前长轮询。
func (s *InvocationService) GetForKey(ctx context.Context, keyID, id uint64, wait time.Duration) (*dto.Invocation, error) {
	inv, err := s.repo.Get(ctx, id)
	if err != nil || inv.APIKeyID == nil || *inv.APIKeyID != keyID {
		if err != nil && !errors.Is(err, repository.ErrNotFound) {
			return nil, err
		}
		return nil, errcode.NotFound.WithMessage("指令不存在")
	}
	if wait > 0 && !inv.Finished() {
		s.wait(ctx, id, min(wait, 30*time.Second))
		if inv, err = s.repo.Get(ctx, id); err != nil {
			return nil, err
		}
	}
	out := dto.InvocationFrom(inv)
	return &out, nil
}

func (s *InvocationService) List(ctx context.Context, f repository.InvocationFilter) (dto.Page[dto.Invocation], error) {
	q := f.ListQuery
	rows, total, err := s.repo.List(ctx, f)
	if err != nil {
		return dto.Page[dto.Invocation]{}, err
	}
	out := make([]dto.Invocation, len(rows))
	for i := range rows {
		out[i] = dto.InvocationFrom(&rows[i])
	}
	return dto.Page[dto.Invocation]{Items: out, Total: total, Page: q.Num(), PageSize: q.Size()}, nil
}

func (s *InvocationService) publish(ctx context.Context, id uint64) {
	if inv, err := s.repo.Get(ctx, id); err == nil {
		s.bus.Publish(TopicInvocationUpdated, dto.InvocationFrom(inv))
	}
}

func (s *InvocationService) wait(ctx context.Context, id uint64, d time.Duration) bool {
	ch := make(chan struct{})
	s.mu.Lock()
	s.waiters[id] = append(s.waiters[id], ch)
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		list := s.waiters[id]
		for i, c := range list {
			if c == ch {
				s.waiters[id] = append(list[:i], list[i+1:]...)
				break
			}
		}
		if len(s.waiters[id]) == 0 {
			delete(s.waiters, id)
		}
		s.mu.Unlock()
	}()
	// 等待期间结果可能已经到达。
	if inv, err := s.repo.Get(ctx, id); err == nil && inv.Finished() {
		return true
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ch:
		return true
	case <-t.C:
		return false
	case <-ctx.Done():
		return false
	}
}

func (s *InvocationService) notify(id uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ch := range s.waiters[id] {
		close(ch)
	}
	delete(s.waiters, id)
}

func newUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
