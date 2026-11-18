package service

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
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

const (
	repoLinkPrefix    = "rks-"
	repoLinkPrefixOld = "rks_" // 旧格式，Sileo 装不上，仍可解析以便吊销 / 用 apt 安装
	repoLinkMaxActive = 50
)

// ErrRepoLinkUnknown 令牌格式正确但没有这个链接（计入封禁）；ErrRepoLinkInvalid 链接存在但已吊销 / 过期 / 用完（不计入）。
// ErrRepoLinkExhausted 下载次数刚用完：宽限期内仍允许断点续传（不算次数），不再允许完整下载。
var (
	ErrRepoLinkUnknown   = errors.New("repo link unknown")
	ErrRepoLinkInvalid   = errors.New("repo link invalid")
	ErrRepoLinkExhausted = errors.New("repo link exhausted")
)

// repoResumeGrace 次数用完后允许续传的时间：下载中途断线时 Sileo 会用 Range 续传。
const repoResumeGrace = 30 * time.Minute

// RepoService 软件源：/repo/<令牌>/ 只提供上传时选择「同步到软件源」的最新版本，供新机器在 Sileo 里首次安装。
type RepoService struct {
	repo   *repository.Set
	sealer *secure.Sealer // 加密保存链接令牌，用于再次查看
	auth   *AuthService
	audit  *AuditService
	log    *zap.Logger
	dir    string
	label  string
	on     bool

	mu    sync.Mutex
	index *RepoIndex
}

// RepoIndex 某个版本对应的源索引（按版本缓存，版本变了才重新生成）。
type RepoIndex struct {
	Release    *model.Release // nil 表示当前没有可提供的版本
	Packages   []byte
	PackagesGz []byte
	ReleaseTxt []byte
	ModTime    time.Time
}

func NewRepoService(r *repository.Set, auth *AuthService, audit *AuditService, sealer *secure.Sealer, log *zap.Logger,
	dir string, enabled bool, label string) *RepoService {
	if strings.TrimSpace(label) == "" {
		label = "ResidentKit"
	}
	return &RepoService{repo: r, auth: auth, audit: audit, sealer: sealer, log: log.Named("repo"), dir: dir, label: label, on: enabled}
}

func (s *RepoService) Enabled() bool { return s.on }

// Label 源名称（Sileo 里显示）。
func (s *RepoService) Label() string { return s.label }

// ---- 控制台 ----

func (s *RepoService) Overview(ctx context.Context, baseURL string) (*dto.RepoOverview, error) {
	links, err := s.repo.RepoLinks.List(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	out := &dto.RepoOverview{Enabled: s.on, Label: s.label, BaseURL: baseURL, Links: make([]dto.RepoLink, 0, len(links))}
	for i := range links {
		out.Links = append(out.Links, dto.RepoLinkFrom(&links[i], now))
	}
	if rel, err := s.latest(ctx); err != nil {
		return nil, err
	} else if rel != nil {
		r := dto.ReleaseFrom(rel)
		out.Latest = &r
	}
	return out, nil
}

func (s *RepoService) Create(ctx context.Context, a Actor, req dto.CreateRepoLinkRequest) (*dto.IssuedRepoLink, error) {
	if !s.on {
		return nil, errcode.Conflict.WithMessage("软件源未开启：在 config.yaml 中设置 repo.enabled: true")
	}
	if err := s.auth.Reauth(ctx, a, req.Password, req.TOTPCode); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, errcode.InvalidParams.WithMessage("请填写名称")
	}
	links, err := s.repo.RepoLinks.List(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	active := 0
	for i := range links {
		if dto.RepoLinkStatus(&links[i], now) == "active" {
			active++
		}
	}
	if active >= repoLinkMaxActive {
		return nil, errcode.Conflict.WithMessage("可用的安装链接已达上限 %d 个，请先吊销不用的", repoLinkMaxActive)
	}
	plain := repoLinkToken()
	l := &model.RepoLink{Name: trunc(name, 64), TokenHash: secure.TokenHash(plain), TokenPrefix: plain[:12],
		MaxDownloads: uint32(req.MaxDownloads), CreatedAt: now}
	if req.ExpiresHours > 0 {
		t := now.Add(time.Duration(req.ExpiresHours) * time.Hour)
		l.ExpiresAt = &t
	}
	if s.sealer != nil {
		if enc, err := s.sealer.Seal(plain); err == nil {
			l.TokenEnc = &enc
		}
	}
	if a.AdminID != 0 {
		id := a.AdminID
		l.CreatedBy = &id
	}
	if err := s.repo.RepoLinks.Create(ctx, l); err != nil {
		return nil, err
	}
	s.audit.Record(ctx, a, AuditRepoLinkCreate, l.Name, true,
		map[string]any{"link": l.ID, "expires_hours": req.ExpiresHours, "max_downloads": req.MaxDownloads})
	return &dto.IssuedRepoLink{RepoLink: dto.RepoLinkFrom(l, now), Path: "/repo/" + plain + "/"}, nil
}

func (s *RepoService) Revoke(ctx context.Context, a Actor, id uint64) error {
	l, err := s.repo.RepoLinks.Get(ctx, id)
	if err != nil {
		return errcode.NotFound
	}
	if err := s.repo.RepoLinks.Revoke(ctx, id, time.Now().UTC()); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return errcode.Conflict.WithMessage("链接已吊销")
		}
		return err
	}
	s.audit.Record(ctx, a, AuditRepoLinkRevoke, l.Name, true, map[string]any{"link": id})
	return nil
}

// Reveal 再次查看链接地址（令牌加密保存；旧版本生成的链接没有保存，无法查看）。
func (s *RepoService) Reveal(ctx context.Context, id uint64) (*dto.RepoLinkPath, error) {
	l, err := s.repo.RepoLinks.Get(ctx, id)
	if err != nil {
		return nil, errcode.NotFound
	}
	if l.RevokedAt != nil {
		return nil, errcode.Conflict.WithMessage("链接已吊销")
	}
	if l.TokenEnc == nil || s.sealer == nil {
		return nil, errcode.Conflict.WithMessage("这个链接是旧版本生成的，无法再次查看，请重新生成")
	}
	plain, err := s.sealer.Open(*l.TokenEnc)
	if err != nil {
		return nil, errcode.Conflict.WithMessage("无法解密链接（加密密钥可能已更换），请重新生成")
	}
	return &dto.RepoLinkPath{Path: "/repo/" + plain + "/"}, nil
}

// Update 修改名称、有效期、下载次数上限；过期或用完的链接改了之后可重新生效。已吊销的不能改。
func (s *RepoService) Update(ctx context.Context, a Actor, id uint64, req dto.UpdateRepoLinkRequest) (*dto.RepoLink, error) {
	l, err := s.repo.RepoLinks.Get(ctx, id)
	if err != nil {
		return nil, errcode.NotFound
	}
	if l.RevokedAt != nil {
		return nil, errcode.Conflict.WithMessage("已吊销的链接不能修改")
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, errcode.InvalidParams.WithMessage("请填写名称")
	}
	now := time.Now().UTC()
	updates := map[string]any{"name": trunc(name, 64), "max_downloads": uint32(req.MaxDownloads)}
	if req.ExpiresHours != nil {
		if *req.ExpiresHours == 0 {
			updates["expires_at"] = nil
		} else {
			updates["expires_at"] = now.Add(time.Duration(*req.ExpiresHours) * time.Hour)
		}
	}
	if err := s.repo.RepoLinks.Update(ctx, id, updates); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errcode.Conflict.WithMessage("链接已吊销")
		}
		return nil, err
	}
	s.audit.Record(ctx, a, AuditRepoLinkUpdate, name, true, map[string]any{"link": id, "expires_hours": req.ExpiresHours, "max_downloads": req.MaxDownloads})
	l, err = s.repo.RepoLinks.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	out := dto.RepoLinkFrom(l, now)
	return &out, nil
}

// Delete 删除已吊销的链接及其下载记录。
func (s *RepoService) Delete(ctx context.Context, a Actor, id uint64) error {
	l, err := s.repo.RepoLinks.Get(ctx, id)
	if err != nil {
		return errcode.NotFound
	}
	ok, err := s.repo.RepoLinks.DeleteRevoked(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		return errcode.Conflict.WithMessage("只能删除已吊销的链接")
	}
	s.audit.Record(ctx, a, AuditRepoLinkDelete, l.Name, true, map[string]any{"link": id})
	return nil
}

func (s *RepoService) Downloads(ctx context.Context, id uint64) ([]dto.RepoDownload, error) {
	if _, err := s.repo.RepoLinks.Get(ctx, id); err != nil {
		return nil, errcode.NotFound
	}
	list, err := s.repo.RepoLinks.Downloads(ctx, id, 100)
	if err != nil {
		return nil, err
	}
	out := make([]dto.RepoDownload, 0, len(list))
	for i := range list {
		out = append(out, dto.RepoDownloadFrom(&list[i]))
	}
	return out, nil
}

// repoLinkToken 安装链接令牌只用小写字母、数字和连字符：
//   - Sileo 经 sileo://source/ 添加源时会把地址转成小写，大小写混合的令牌会被改坏；
//   - 不能有下划线：APT 给列表文件命名时把 _ 转义成 %5f，Sileo 不转义，
//     安装时 APT 找不到 Sileo 写的包列表，报「Unable to locate package」。
//
// 32 字节随机数的十六进制，强度与其他令牌相同。
func repoLinkToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return repoLinkPrefix + hex.EncodeToString(b)
}

// ---- 软件源（公开，凭链接令牌）----

// Resolve 校验链接令牌：不存在返回 ErrRepoLinkUnknown，已吊销 / 过期 / 用完返回 ErrRepoLinkInvalid；
// 次数刚用完（宽限期内）返回链接和 ErrRepoLinkExhausted，调用方只允许续传。
func (s *RepoService) Resolve(ctx context.Context, token string) (*model.RepoLink, error) {
	token = strings.ToLower(token) // 令牌全小写，客户端改了大小写也能认
	if !strings.HasPrefix(token, repoLinkPrefix) && !strings.HasPrefix(token, repoLinkPrefixOld) || len(token) < 20 || len(token) > 128 {
		return nil, ErrRepoLinkUnknown
	}
	l, err := s.repo.RepoLinks.FindByHash(ctx, secure.TokenHash(token))
	if errors.Is(err, repository.ErrNotFound) {
		return nil, ErrRepoLinkUnknown
	}
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	switch dto.RepoLinkStatus(l, now) {
	case "active":
		return l, nil
	case "exhausted":
		if l.LastUsedAt != nil && now.Sub(*l.LastUsedAt) < repoResumeGrace {
			return l, ErrRepoLinkExhausted
		}
	}
	return nil, ErrRepoLinkInvalid
}

// Index 当前的源索引。没有可提供的版本时 Packages 为空（Sileo 里搜不到）。
func (s *RepoService) Index(ctx context.Context) (*RepoIndex, error) {
	rel, err := s.latest(ctx)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.index != nil && sameRelease(s.index.Release, rel) {
		return s.index, nil
	}
	idx, err := s.buildIndex(rel)
	if err != nil {
		return nil, err
	}
	s.index = idx
	return idx, nil
}

// DownloadMeta 下载请求的来源信息（记录用）。
type DownloadMeta struct {
	IP, Machine, Firmware, UserAgent string
}

// OpenDeb 下载安装包：只允许当前提供的版本。counted 为 true 时占用一次下载名额并记录（名额已用完返回 ErrRepoLinkInvalid）。
func (s *RepoService) OpenDeb(ctx context.Context, l *model.RepoLink, file string, counted bool, m DownloadMeta) (*os.File, *model.Release, error) {
	idx, err := s.Index(ctx)
	if err != nil {
		return nil, nil, err
	}
	rel := idx.Release
	if rel == nil || file != filepath.Base(rel.FileName) {
		return nil, nil, errcode.NotFound
	}
	f, err := os.Open(filepath.Join(s.dir, filepath.Base(rel.FileName)))
	if err != nil {
		return nil, nil, errcode.NotFound
	}
	if !counted {
		return f, rel, nil
	}
	now := time.Now().UTC()
	ok, err := s.repo.RepoLinks.TakeDownload(ctx, l.ID, now)
	if err != nil || !ok {
		f.Close()
		if err != nil {
			return nil, nil, err
		}
		return nil, nil, ErrRepoLinkInvalid
	}
	d := &model.RepoDownload{LinkID: l.ID, Version: rel.Version, IP: trunc(m.IP, 45), Machine: trunc(m.Machine, 32),
		Firmware: trunc(m.Firmware, 16), UserAgent: trunc(m.UserAgent, 255), CreatedAt: now}
	if err := s.repo.RepoLinks.AddDownload(ctx, d); err != nil {
		s.log.Warn("record download", zap.Error(err))
	}
	s.log.Info("repo download", zap.Uint64("link", l.ID), zap.String("version", rel.Version), zap.String("ip", m.IP))
	return f, rel, nil
}

// latest 源里提供的版本：上传时选择同步到软件源、且未撤回的最新版本；没有则返回 nil。
func (s *RepoService) latest(ctx context.Context) (*model.Release, error) {
	r, err := s.repo.Releases.LatestInRepo(ctx)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, nil
	}
	return r, err
}

func sameRelease(a, b *model.Release) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.ID == b.ID
}

func (s *RepoService) buildIndex(rel *model.Release) (*RepoIndex, error) {
	idx := &RepoIndex{Release: rel, ModTime: time.Now().UTC().Truncate(time.Second)}
	if rel != nil {
		idx.ModTime = rel.CreatedAt.UTC().Truncate(time.Second)
		entry, err := s.packageEntry(rel)
		if err != nil {
			return nil, err
		}
		idx.Packages = entry
	}
	var gz bytes.Buffer
	w, _ := gzip.NewWriterLevel(&gz, gzip.BestCompression)
	_, _ = w.Write(idx.Packages)
	_ = w.Close()
	idx.PackagesGz = gz.Bytes()

	var b strings.Builder
	fmt.Fprintf(&b, "Origin: %s\nLabel: %s\nSuite: stable\nVersion: 1.0\nCodename: ios\n", s.label, s.label)
	fmt.Fprintf(&b, "Date: %s\n", idx.ModTime.UTC().Format(time.RFC1123)) // APT 缺了会警告 Invalid 'Date' entry
	fmt.Fprintf(&b, "Architectures: iphoneos-arm64\nComponents: main\nDescription: %s\n", s.label)
	files := []struct {
		name string
		data []byte
	}{{"Packages", idx.Packages}, {"Packages.gz", idx.PackagesGz}}
	for _, algo := range []struct {
		title string
		h     func() hash.Hash
	}{{"MD5Sum", md5.New}, {"SHA256", sha256.New}} {
		b.WriteString(algo.title + ":\n")
		for _, f := range files {
			h := algo.h()
			h.Write(f.data)
			fmt.Fprintf(&b, " %s %d %s\n", hex.EncodeToString(h.Sum(nil)), len(f.data), f.name)
		}
	}
	idx.ReleaseTxt = []byte(b.String())
	return idx, nil
}

// packageEntry Packages 里的一条：安装包自带的 control，加上文件位置、大小和摘要。
func (s *RepoService) packageEntry(rel *model.Release) ([]byte, error) {
	path := filepath.Join(s.dir, filepath.Base(rel.FileName))
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open release %s: %w", rel.Version, err)
	}
	defer f.Close()
	m5, s1, s256 := md5.New(), sha1.New(), sha256.New()
	size, err := io.Copy(io.MultiWriter(m5, s1, s256), f)
	if err != nil {
		return nil, err
	}
	ctrl, err := release.ReadControl(path)
	if err != nil {
		s.log.Warn("read control, using minimal fields", zap.String("version", rel.Version), zap.Error(err))
		ctrl = fmt.Sprintf("Package: %s\nName: %s\nVersion: %s\nArchitecture: iphoneos-arm64\nDescription: %s",
			rel.Package, s.label, rel.Version, s.label)
	}
	var b strings.Builder
	for _, line := range strings.Split(ctrl, "\n") {
		key, _, _ := strings.Cut(line, ":")
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "filename", "size", "md5sum", "sha1", "sha256":
			continue // 以实际文件为准
		}
		b.WriteString(line + "\n")
	}
	fmt.Fprintf(&b, "Filename: debs/%s\nSize: %d\nMD5sum: %s\nSHA1: %s\nSHA256: %s\n\n",
		filepath.Base(rel.FileName), size, hex.EncodeToString(m5.Sum(nil)), hex.EncodeToString(s1.Sum(nil)),
		hex.EncodeToString(s256.Sum(nil)))
	return []byte(b.String()), nil
}
