package service

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/icloudza/ResidentKit/server/internal/dto"
	"github.com/icloudza/ResidentKit/server/internal/model"
	"github.com/icloudza/ResidentKit/server/internal/pkg/errcode"
	"github.com/icloudza/ResidentKit/server/internal/release"
	"github.com/icloudza/ResidentKit/server/internal/repository"
)

// 设备在下载 / 安装阶段超过这个时间没有回报，视为失败。
const updateStallAfter = 20 * time.Minute

// 超过这个时间仍未收到结果事件，才按设备上报的版本推断已安装（须长于设备端健康检查的 90 + 20 秒）。
const versionTrustAfter = 5 * time.Minute

var otaActor = Actor{Username: "ota"}

// OTAService 发布版本、灰度计划与设备更新。
//
// 流程：上传（校验签名）→ 创建灰度计划 → 引擎对在线、版本较低的目标设备下发
// system.update/install → 设备下载、校验、安装后以 system.update.* 事件回报。
// 引擎由定时任务、设备上线、计划变更触发，同一时刻只有一个在运行。
type OTAService struct {
	repo    *repository.Set
	devices *DeviceService
	inv     *InvocationService
	auth    *AuthService
	audit   *AuditService
	bus     *Bus
	log     *zap.Logger
	pub     *ecdsa.PublicKey // nil 表示未配置，OTA 关闭
	pubB64  string
	dir     string

	mu sync.Mutex // 串行化引擎
}

func NewOTAService(r *repository.Set, devices *DeviceService, inv *InvocationService, auth *AuthService,
	audit *AuditService, bus *Bus, log *zap.Logger, publicKey, dir string) (*OTAService, error) {
	s := &OTAService{repo: r, devices: devices, inv: inv, auth: auth, audit: audit, bus: bus,
		log: log.Named("ota"), dir: dir}
	if strings.TrimSpace(publicKey) != "" {
		k, err := release.ParsePublicKey(publicKey)
		if err != nil {
			return nil, fmt.Errorf("release.public_key: %w", err)
		}
		s.pub, s.pubB64 = k, strings.TrimSpace(publicKey)
	}
	return s, nil
}

func (s *OTAService) Config() dto.OTAConfig {
	return dto.OTAConfig{Enabled: s.pub != nil, PublicKey: s.pubB64}
}

var errOTADisabled = errcode.Conflict.WithMessage("服务器未配置发布公钥")

// ---- 上传 ----

// CheckUploader 上传前再次验证身份（在接收文件之前调用，不合格的不接收）。
func (s *OTAService) CheckUploader(ctx context.Context, a Actor, password, totpCode string) error {
	if s.pub == nil {
		return errOTADisabled
	}
	return s.auth.Reauth(ctx, a, password, totpCode)
}

// Upload 接收发布包（rk-release sign 生成的 .rkrel.zip）：校验清单签名与版本号，
// 解出安装包并核对大小与哈希，全部通过才入库。调用前须先 CheckUploader。
// Upload inRepo：同步到软件源，源里改为提供这个版本（不同步则源里仍是之前的版本）。
func (s *OTAService) Upload(ctx context.Context, a Actor, notes string, inRepo bool, bundle io.Reader) (*dto.Release, error) {
	if s.pub == nil {
		return nil, errOTADisabled
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return nil, err
	}
	// zip 的目录在文件末尾，先完整落盘再解析。
	zf, err := os.CreateTemp(s.dir, "upload-*.zip")
	if err != nil {
		return nil, err
	}
	defer os.Remove(zf.Name())
	n, err := io.Copy(zf, io.LimitReader(bundle, maxBundleBytes+1))
	if cerr := zf.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return nil, errcode.InvalidParams.WithMessage("接收发布包失败")
	}
	if n > maxBundleBytes {
		return nil, errcode.InvalidParams.WithMessage("发布包过大（上限 64 MB）")
	}

	b, err := release.OpenBundle(zf.Name())
	if err != nil {
		return nil, errcode.InvalidParams.WithMessage("%s", err.Error())
	}
	defer b.Close()
	m := b.Manifest
	if err := m.Validate(); err != nil {
		return nil, errcode.InvalidParams.WithMessage("%s", err.Error())
	}
	if err := release.Verify(s.pub, m); err != nil {
		s.audit.Record(ctx, a, AuditReleaseUpload, m.Version, false, map[string]string{"reason": "bad signature"})
		return nil, errcode.InvalidParams.WithMessage("%s", err.Error())
	}
	all, err := s.repo.Releases.All(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range all {
		if release.CompareVersions(m.Version, r.Version) <= 0 {
			return nil, errcode.Conflict.WithMessage("版本 %s 必须高于已发布的 %s", m.Version, r.Version)
		}
	}

	name := m.Version + ".deb"
	tmpDeb := filepath.Join(s.dir, "upload-"+name)
	defer os.Remove(tmpDeb) // 成功时已链接到正式文件名，删除的只是临时名
	if err := b.ExtractPackage(tmpDeb); err != nil {
		s.audit.Record(ctx, a, AuditReleaseUpload, m.Version, false, map[string]string{"reason": "hash mismatch"})
		return nil, errcode.InvalidParams.WithMessage("%s", err.Error())
	}
	// 用硬链接落地：目标已存在（同一版本并发上传）时失败，不会覆盖另一个上传的文件。
	if err := os.Link(tmpDeb, filepath.Join(s.dir, name)); err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, errcode.Conflict.WithMessage("版本 %s 正在上传或已存在", m.Version)
		}
		return nil, err
	}
	now := time.Now().UTC()
	r := &model.Release{Version: m.Version, Package: m.Package, Size: m.Size, SHA256: m.SHA256,
		Signature: m.Signature, Notes: trunc(strings.TrimSpace(notes), 2000), FileName: name,
		Status: model.ReleasePublished, InRepo: inRepo, CreatedAt: now, UpdatedAt: now}
	if a.AdminID != 0 {
		id := a.AdminID
		r.CreatedBy = &id
	}
	if err := s.repo.Releases.Create(ctx, r); err != nil {
		_ = os.Remove(filepath.Join(s.dir, name))
		if strings.Contains(err.Error(), "Duplicate") {
			return nil, errcode.Conflict.WithMessage("版本 %s 已存在", m.Version)
		}
		return nil, err
	}
	s.audit.Record(ctx, a, AuditReleaseUpload, r.Version, true, map[string]any{"release": r.ID, "size": r.Size, "repo": inRepo})
	out := dto.ReleaseFrom(r)
	return &out, nil
}

// 发布包上限：安装包 64 MB 加少量开销。
const maxBundleBytes = 64<<20 + 64<<10

// ---- 查询 ----

func (s *OTAService) List(ctx context.Context) ([]dto.Release, error) {
	all, err := s.repo.Releases.All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]dto.Release, len(all))
	idx := map[uint64]int{}
	ids := make([]uint64, len(all))
	for i := range all {
		out[i] = dto.ReleaseFrom(&all[i])
		idx[all[i].ID] = i
		ids[i] = all[i].ID
	}
	counts, err := s.repo.DeviceUpdates.Counts(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, c := range counts {
		if i, ok := idx[c.ReleaseID]; ok {
			out[i].Counts[c.Status] = c.Count
		}
	}
	return out, nil
}

func (s *OTAService) getRelease(ctx context.Context, id uint64) (*model.Release, error) {
	r, err := s.repo.Releases.Get(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, errcode.NotFound.WithMessage("版本不存在")
	}
	return r, err
}

func (s *OTAService) Detail(ctx context.Context, id uint64) (*dto.ReleaseDetail, error) {
	r, err := s.getRelease(ctx, id)
	if err != nil {
		return nil, err
	}
	rolls, err := s.repo.Rollouts.ListByRelease(ctx, id)
	if err != nil {
		return nil, err
	}
	ups, err := s.repo.DeviceUpdates.ListByRelease(ctx, id)
	if err != nil {
		return nil, err
	}
	out := &dto.ReleaseDetail{Release: dto.ReleaseFrom(r), Rollouts: make([]dto.Rollout, len(rolls)),
		Devices: make([]dto.DeviceUpdate, len(ups))}
	for i := range rolls {
		out.Rollouts[i] = dto.RolloutFrom(&rolls[i])
	}
	for i := range ups {
		out.Devices[i] = dto.DeviceUpdateFrom(&ups[i], r.Version)
		out.Release.Counts[ups[i].Status]++
	}
	return out, nil
}

// DeviceInfo 设备当前版本、可更新到的版本与更新历史。
func (s *OTAService) DeviceInfo(ctx context.Context, deviceID uint64) (*dto.DeviceUpdateInfo, error) {
	d, err := s.devices.Get(ctx, deviceID)
	if err != nil {
		return nil, err
	}
	all, err := s.repo.Releases.All(ctx)
	if err != nil {
		return nil, err
	}
	out := &dto.DeviceUpdateInfo{Version: d.Version, Supported: supportsUpdate(d), History: []dto.DeviceUpdate{}}
	versions := map[uint64]string{}
	for i := range all {
		versions[all[i].ID] = all[i].Version
		if out.Latest == nil && all[i].Status == model.ReleasePublished {
			r := dto.ReleaseFrom(&all[i])
			out.Latest = &r
		}
	}
	ups, err := s.repo.DeviceUpdates.ListByDevice(ctx, deviceID, 20)
	if err != nil {
		return nil, err
	}
	for i := range ups {
		out.History = append(out.History, dto.DeviceUpdateFrom(&ups[i], versions[ups[i].ReleaseID]))
	}
	return out, nil
}

// ---- 管理 ----

// Withdraw 撤回版本：停止所有相关计划，已下发的不受影响；文件保留以便设备完成正在进行的下载。
func (s *OTAService) Withdraw(ctx context.Context, a Actor, id uint64) error {
	r, err := s.getRelease(ctx, id)
	if err != nil {
		return err
	}
	if r.Status == model.ReleaseWithdrawn {
		return nil
	}
	if err := s.repo.Releases.SetStatus(ctx, id, model.ReleaseWithdrawn); err != nil {
		return err
	}
	rolls, _ := s.repo.Rollouts.ListByRelease(ctx, id)
	for _, ro := range rolls {
		_, _ = s.repo.Rollouts.Transition(ctx, ro.ID, []string{model.RolloutActive, model.RolloutPaused}, model.RolloutCancelled)
	}
	s.audit.Record(ctx, a, AuditReleaseRevoke, r.Version, true, map[string]any{"release": id})
	return nil
}

// SetInRepo 同步到软件源或从源里移除（源里提供已同步、未撤回的最新版本）。
func (s *OTAService) SetInRepo(ctx context.Context, a Actor, id uint64, inRepo bool) error {
	r, err := s.getRelease(ctx, id)
	if err != nil {
		return err
	}
	if inRepo && r.Status != model.ReleasePublished {
		return errcode.Conflict.WithMessage("已撤回的版本不能同步到软件源")
	}
	if err := s.repo.Releases.SetInRepo(ctx, id, inRepo); err != nil {
		return err
	}
	s.audit.Record(ctx, a, AuditReleaseRepo, r.Version, true, map[string]any{"release": id, "in_repo": inRepo})
	return nil
}

// CreateRollout 创建灰度计划并立即运行一次引擎。
func (s *OTAService) CreateRollout(ctx context.Context, a Actor, releaseID uint64, req dto.CreateRolloutRequest) (*dto.Rollout, error) {
	r, err := s.getRelease(ctx, releaseID)
	if err != nil {
		return nil, err
	}
	if r.Status != model.ReleasePublished {
		return nil, errcode.Conflict.WithMessage("版本已撤回")
	}
	if _, err := os.Stat(filepath.Join(s.dir, filepath.Base(r.FileName))); err != nil {
		return nil, errcode.Conflict.WithMessage("该版本的安装包已清理")
	}
	ro := &model.Rollout{ReleaseID: releaseID, Target: req.Target, Status: model.RolloutActive}
	if req.Target == "devices" {
		if len(req.DeviceIDs) == 0 {
			return nil, errcode.InvalidParams.WithMessage("请选择设备")
		}
		ids := slices.Compact(slices.Sorted(slices.Values(req.DeviceIDs)))
		ro.DeviceIDs, _ = json.Marshal(ids)
		// 指定设备时视为重试：清掉这些设备在该版本上失败 / 回滚的记录。
		for _, did := range ids {
			if u, err := s.repo.DeviceUpdates.Find(ctx, did, releaseID); err == nil &&
				u.Finished() && u.Status != model.UpdInstalled {
				_ = s.repo.DeviceUpdates.Delete(ctx, u.ID)
			}
		}
	}
	ro.CreatedAt = time.Now().UTC()
	ro.UpdatedAt = ro.CreatedAt
	if a.AdminID != 0 {
		id := a.AdminID
		ro.CreatedBy = &id
	}
	if err := s.repo.Rollouts.Create(ctx, ro); err != nil {
		return nil, err
	}
	s.audit.Record(ctx, a, AuditRolloutCreate, r.Version, true,
		map[string]any{"rollout": ro.ID, "target": ro.Target, "devices": req.DeviceIDs})
	go s.TickDetached()
	out := dto.RolloutFrom(ro)
	return &out, nil
}

// ChangeRollout 暂停 / 继续 / 取消。
func (s *OTAService) ChangeRollout(ctx context.Context, a Actor, id uint64, action string) error {
	ro, err := s.repo.Rollouts.Get(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return errcode.NotFound.WithMessage("计划不存在")
	}
	if err != nil {
		return err
	}
	var from []string
	var to string
	switch action {
	case "pause":
		from, to = []string{model.RolloutActive}, model.RolloutPaused
	case "resume":
		from, to = []string{model.RolloutPaused}, model.RolloutActive
		if r, err := s.getRelease(ctx, ro.ReleaseID); err != nil || r.Status != model.ReleasePublished {
			return errcode.Conflict.WithMessage("版本已撤回")
		}
	case "cancel":
		from, to = []string{model.RolloutActive, model.RolloutPaused}, model.RolloutCancelled
	default:
		return errcode.InvalidParams
	}
	ok, err := s.repo.Rollouts.Transition(ctx, id, from, to)
	if err != nil {
		return err
	}
	if !ok {
		return errcode.Conflict.WithMessage("计划当前状态不能执行此操作")
	}
	s.audit.Record(ctx, a, AuditRolloutChange, fmt.Sprint(id), true, map[string]string{"action": action})
	if to == model.RolloutActive {
		go s.TickDetached()
	}
	return nil
}

// Retry 重试一台设备的失败更新：删除记录后立即重新下发。
func (s *OTAService) Retry(ctx context.Context, a Actor, updateID uint64) error {
	u, err := s.repo.DeviceUpdates.Get(ctx, updateID)
	if errors.Is(err, repository.ErrNotFound) {
		return errcode.NotFound.WithMessage("更新记录不存在")
	}
	if err != nil {
		return err
	}
	if !u.Finished() || u.Status == model.UpdInstalled {
		return errcode.Conflict.WithMessage("只能重试失败的更新")
	}
	r, err := s.getRelease(ctx, u.ReleaseID)
	if err != nil {
		return err
	}
	if r.Status != model.ReleasePublished {
		return errcode.Conflict.WithMessage("版本已撤回")
	}
	d, err := s.devices.Get(ctx, u.DeviceID)
	if err != nil {
		return err
	}
	if !d.Online {
		return errcode.DeviceOffline
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.repo.DeviceUpdates.Delete(ctx, u.ID); err != nil {
		return err
	}
	s.audit.Record(ctx, a, AuditUpdateRetry, fmt.Sprint(u.DeviceID), true, map[string]any{"release": r.Version})
	s.start(ctx, u.RolloutID, r, d)
	return nil
}

// ---- 引擎 ----

// TickDetached 后台运行一次引擎。
func (s *OTAService) TickDetached() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	s.Tick(ctx)
}

// Tick 收敛状态并为每台设备下发它应得的最高版本。
func (s *OTAService) Tick(ctx context.Context) {
	if s.pub == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	busy := s.reconcile(ctx)

	rolls, err := s.repo.Rollouts.Active(ctx)
	if err != nil || len(rolls) == 0 {
		return
	}
	devs, err := s.repo.Devices.List(ctx)
	if err != nil {
		return
	}
	// 一次取出涉及的版本与它们的全部更新记录，避免逐台设备查询。
	releases := map[uint64]*model.Release{}
	var ids []uint64
	for _, ro := range rolls {
		if _, ok := releases[ro.ReleaseID]; ok {
			continue
		}
		if rel, err := s.repo.Releases.Get(ctx, ro.ReleaseID); err == nil {
			releases[ro.ReleaseID] = rel
			ids = append(ids, rel.ID)
		}
	}
	ups, err := s.repo.DeviceUpdates.ListByReleases(ctx, ids)
	if err != nil {
		return
	}
	type key struct{ dev, rel uint64 }
	existing := make(map[key]*model.DeviceUpdate, len(ups))
	for i := range ups {
		existing[key{ups[i].DeviceID, ups[i].ReleaseID}] = &ups[i]
	}
	// 面向全部设备的计划只保留版本最高的一个，更早的视为已被取代、自动完成，不再每轮扫描。
	var newestAll *model.Release
	for _, ro := range rolls {
		if rel := releases[ro.ReleaseID]; ro.Target == "all" && rel != nil && rel.Status == model.ReleasePublished &&
			(newestAll == nil || release.CompareVersions(rel.Version, newestAll.Version) > 0) {
			newestAll = rel
		}
	}

	type pick struct {
		ro  *model.Rollout
		rel *model.Release
	}
	best := map[uint64]pick{} // 设备 → 应安装的最高版本
	for i := range rolls {
		ro := &rolls[i]
		rel := releases[ro.ReleaseID]
		if rel == nil {
			continue
		}
		if rel.Status != model.ReleasePublished {
			_, _ = s.repo.Rollouts.Transition(ctx, ro.ID, []string{model.RolloutActive}, model.RolloutCancelled)
			continue
		}
		if ro.Target == "all" && newestAll != nil && rel.ID != newestAll.ID &&
			release.CompareVersions(rel.Version, newestAll.Version) < 0 {
			_, _ = s.repo.Rollouts.Transition(ctx, ro.ID, []string{model.RolloutActive}, model.RolloutCompleted)
			continue
		}
		targets := dto.RolloutFrom(ro).DeviceIDs
		done := true
		for j := range devs {
			d := &devs[j]
			if ro.Target == "devices" && !slices.Contains(targets, d.ID) {
				continue
			}
			if release.CompareVersions(d.Version, rel.Version) >= 0 {
				continue
			}
			if u := existing[key{d.ID, rel.ID}]; u != nil {
				if !u.Finished() {
					done = false
				}
				continue
			}
			done = false
			if cur, ok := best[d.ID]; !ok || release.CompareVersions(rel.Version, cur.rel.Version) > 0 {
				best[d.ID] = pick{ro, rel}
			}
		}
		// 指定设备的计划在全部设备有结果后完成；面向全部设备的计划持续生效（新设备上线也会更新），
		// 直到被更高版本的全量计划取代。
		if ro.Target == "devices" && done {
			_, _ = s.repo.Rollouts.Transition(ctx, ro.ID, []string{model.RolloutActive}, model.RolloutCompleted)
		}
	}
	for j := range devs {
		d := &devs[j]
		p, ok := best[d.ID]
		if !ok || !d.Online || busy[d.ID] {
			continue
		}
		s.start(ctx, p.ro.ID, p.rel, d)
	}
}

// reconcile 根据指令结果与超时推进未完成的更新，返回仍在进行中的设备。
func (s *OTAService) reconcile(ctx context.Context) map[uint64]bool {
	busy := map[uint64]bool{}
	list, err := s.repo.DeviceUpdates.Unfinished(ctx)
	if err != nil {
		return busy
	}
	for i := range list {
		u := &list[i]
		if u.Status == model.UpdQueued && u.InvocationID != nil {
			inv, err := s.inv.Get(ctx, *u.InvocationID)
			if err == nil {
				switch inv.Status {
				case model.InvSucceeded:
					s.transition(ctx, u, []string{model.UpdQueued}, model.UpdDownloading, "")
					continue
				case model.InvFailed, model.InvExpired, model.InvLost:
					msg := inv.ErrorMessage
					if msg == "" {
						msg = "设备未接受更新指令"
					}
					s.transition(ctx, u, []string{model.UpdQueued}, model.UpdFailed, msg)
					continue
				}
			}
		}
		// 兜底：结果事件丢失时，健康检查窗口（约 2 分钟）过后按设备上报的版本判定已安装。
		// 窗口内不能这样判断：新版本可能连上来之后才崩溃并被回滚。
		if u.Status == model.UpdDownloading && time.Since(u.UpdatedAt) > versionTrustAfter {
			if d, err := s.repo.Devices.Get(ctx, u.DeviceID); err == nil {
				if r, err := s.repo.Releases.Get(ctx, u.ReleaseID); err == nil && release.CompareVersions(d.Version, r.Version) >= 0 {
					s.transition(ctx, u, []string{model.UpdDownloading}, model.UpdInstalled, "")
					continue
				}
			}
		}
		if time.Since(u.UpdatedAt) > updateStallAfter {
			s.transition(ctx, u, []string{model.UpdQueued, model.UpdDownloading}, model.UpdFailed, "超时：设备未回报结果")
			continue
		}
		busy[u.DeviceID] = true
	}
	return busy
}

// start 为一台在线设备下发安装指令。调用方持有 s.mu。
func (s *OTAService) start(ctx context.Context, rolloutID uint64, rel *model.Release, d *model.Device) {
	now := time.Now().UTC()
	u := &model.DeviceUpdate{DeviceID: d.ID, ReleaseID: rel.ID, RolloutID: rolloutID, Status: model.UpdQueued,
		CreatedAt: now, UpdatedAt: now}
	if ok, err := s.repo.DeviceUpdates.CreateIgnore(ctx, u); err != nil || !ok {
		return
	}
	if !supportsUpdate(d) {
		s.transition(ctx, u, nil, model.UpdUnsupported, fmt.Sprintf("设备版本 %s 不支持在线更新，请手动安装一次", d.Version))
		return
	}
	inv, err := s.inv.Create(ctx, otaActor, d.ID, dto.InvokeRequest{
		Module: "system.update", Command: "install", TTLSeconds: -1,
		Arguments: map[string]any{"release_id": rel.ID, "version": rel.Version, "size": rel.Size,
			"sha256": rel.SHA256, "signature": rel.Signature},
	})
	if errors.Is(err, errcode.DeviceOffline) {
		_ = s.repo.DeviceUpdates.Delete(ctx, u.ID) // 下次上线再来
		return
	}
	if err != nil {
		s.transition(ctx, u, nil, model.UpdFailed, errcode.From(err).Message)
		return
	}
	_, _ = s.repo.DeviceUpdates.Transition(ctx, u.ID, nil, map[string]any{"invocation_id": inv.ID})
	s.log.Info("update dispatched", zap.Uint64("device", d.ID), zap.String("version", rel.Version))
	s.publish(ctx, u.ID)
}

func (s *OTAService) transition(ctx context.Context, u *model.DeviceUpdate, from []string, to, msg string) {
	ok, err := s.repo.DeviceUpdates.Transition(ctx, u.ID, from, map[string]any{"status": to, "error": trunc(msg, 255)})
	if err == nil && ok {
		s.publish(ctx, u.ID)
	}
}

func (s *OTAService) publish(ctx context.Context, id uint64) {
	u, err := s.repo.DeviceUpdates.Get(ctx, id)
	if err != nil {
		return
	}
	version := ""
	if r, err := s.repo.Releases.Get(ctx, u.ReleaseID); err == nil {
		version = r.Version
	}
	s.bus.Publish(TopicUpdateChanged, dto.DeviceUpdateFrom(u, version))
}

func supportsUpdate(d *model.Device) bool {
	return slices.Contains(dto.DeviceFrom(d).RemoteCommands["system.update"], "install")
}

// ---- 设备侧回调 ----

// OnDeviceOnline 设备上线：运行一次引擎（离线期间的计划此时下发）。
func (s *OTAService) OnDeviceOnline(d *model.Device) {
	if s.pub == nil {
		return
	}
	go s.TickDetached()
}

// OnEvent 处理 system.update.* 事件。事件是设备的权威结果，可覆盖按版本号推断的状态。
func (s *OTAService) OnEvent(ctx context.Context, deviceID uint64, typ string, payload json.RawMessage) {
	if !strings.HasPrefix(typ, "system.update.") {
		return
	}
	var p struct {
		ReleaseID uint64 `json:"release_id"`
		Error     string `json:"error"`
		Stage     string `json:"stage"`
	}
	if json.Unmarshal(payload, &p) != nil || p.ReleaseID == 0 {
		return
	}
	u, err := s.repo.DeviceUpdates.Find(ctx, deviceID, p.ReleaseID)
	if err != nil {
		return
	}
	msg := p.Error
	if msg != "" && p.Stage != "" {
		msg = stageName(p.Stage) + "：" + msg
	}
	switch typ {
	case "system.update.started":
		s.transition(ctx, u, []string{model.UpdQueued}, model.UpdDownloading, "")
	case "system.update.installed":
		s.transition(ctx, u, nil, model.UpdInstalled, "")
	case "system.update.rolled_back":
		s.transition(ctx, u, nil, model.UpdRolledBack, msg)
	case "system.update.failed":
		s.transition(ctx, u, []string{model.UpdQueued, model.UpdDownloading, model.UpdFailed}, model.UpdFailed, msg)
	}
}

func stageName(s string) string {
	switch s {
	case "download":
		return "下载"
	case "verify":
		return "校验"
	case "install":
		return "安装"
	}
	return s
}

// ---- 清理 ----

// keepReleaseFiles 始终保留安装包的最新版本数。
const keepReleaseFiles = 5

// PruneFiles 删除不再需要的安装包文件（版本记录保留）。保留：最新的几个版本、仍有设备在运行的版本
// （首次 OTA 前设备要下载当前版本作回滚包）、进行中的灰度与更新用到的版本。
func (s *OTAService) PruneFiles(ctx context.Context) {
	all, err := s.repo.Releases.All(ctx)
	if err != nil {
		return
	}
	devs, err := s.devices.Models(ctx)
	if err != nil {
		return
	}
	active, err := s.repo.Rollouts.Active(ctx)
	if err != nil {
		return
	}
	unfinished, err := s.repo.DeviceUpdates.Unfinished(ctx)
	if err != nil {
		return
	}
	running := map[string]bool{}
	for _, d := range devs {
		running[d.Version] = true
	}
	inUse := map[uint64]bool{}
	for _, ro := range active {
		inUse[ro.ReleaseID] = true
	}
	for _, u := range unfinished {
		inUse[u.ReleaseID] = true
	}
	for i, r := range all { // 新到旧
		if i < keepReleaseFiles || running[r.Version] || inUse[r.ID] {
			continue
		}
		path := filepath.Join(s.dir, filepath.Base(r.FileName))
		if err := os.Remove(path); err == nil {
			s.log.Info("release file pruned", zap.String("version", r.Version))
		} else if !errors.Is(err, os.ErrNotExist) {
			s.log.Warn("prune release file", zap.String("version", r.Version), zap.Error(err))
		}
	}
}

// ---- 下载 ----

// OpenVersion 设备下载自己当前版本的安装包（首次 OTA 前缓存为回滚包）。不限状态：已撤回的版本也可能正在运行。
func (s *OTAService) OpenVersion(ctx context.Context, tok *model.DeviceToken, version string) (*os.File, *model.Release, error) {
	if tok.DeviceID == nil {
		return nil, nil, errcode.Forbidden
	}
	r, err := s.repo.Releases.GetByVersion(ctx, version)
	if err != nil {
		return nil, nil, errcode.NotFound
	}
	f, err := os.Open(filepath.Join(s.dir, filepath.Base(r.FileName)))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, errcode.NotFound
	}
	if err != nil {
		return nil, nil, err
	}
	return f, r, nil
}

// OpenDownload 设备下载安装包：令牌须已绑定设备，且该设备有这个版本进行中的更新。
func (s *OTAService) OpenDownload(ctx context.Context, tok *model.DeviceToken, releaseID uint64) (*os.File, *model.Release, error) {
	if tok.DeviceID == nil {
		return nil, nil, errcode.Forbidden
	}
	u, err := s.repo.DeviceUpdates.Find(ctx, *tok.DeviceID, releaseID)
	if err != nil || u.Finished() {
		return nil, nil, errcode.Forbidden
	}
	r, err := s.repo.Releases.Get(ctx, releaseID)
	if err != nil {
		return nil, nil, errcode.NotFound
	}
	f, err := os.Open(filepath.Join(s.dir, filepath.Base(r.FileName)))
	if err != nil {
		return nil, nil, err
	}
	return f, r, nil
}
