package service

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/icloudza/ResidentKit/server/internal/dto"
	"github.com/icloudza/ResidentKit/server/internal/model"
	"github.com/icloudza/ResidentKit/server/internal/pkg/secure"
	"github.com/icloudza/ResidentKit/server/internal/repository"
)

type repoReleases struct {
	repository.ReleaseRepository
	m map[uint64]*model.Release
}

// LatestInRepo 与真实仓储一致：已同步到源、未撤回、id 最大的一个。
func (f repoReleases) LatestInRepo(context.Context) (*model.Release, error) {
	var best *model.Release
	for _, r := range f.m {
		if r.InRepo && r.Status == model.ReleasePublished && (best == nil || r.ID > best.ID) {
			best = r
		}
	}
	if best == nil {
		return nil, repository.ErrNotFound
	}
	return best, nil
}

func writeDeb(t *testing.T, dir, name, control string) {
	t.Helper()
	var ctl bytes.Buffer
	gz := gzip.NewWriter(&ctl)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "./control", Mode: 0o644, Size: int64(len(control))})
	_, _ = tw.Write([]byte(control))
	_ = tw.Close()
	_ = gz.Close()
	var out bytes.Buffer
	out.WriteString("!<arch>\n")
	for _, m := range []struct {
		n string
		d []byte
	}{{"debian-binary", []byte("2.0\n")}, {"control.tar.gz", ctl.Bytes()}} {
		fmt.Fprintf(&out, "%-16s%-12d%-6d%-6d%-8s%-10d`\n", m.n, 0, 0, 0, "100644", len(m.d))
		out.Write(m.d)
		if len(m.d)%2 == 1 {
			out.WriteByte('\n')
		}
	}
	if err := os.WriteFile(filepath.Join(dir, name), out.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRepoIndexServesLatestSyncedRelease(t *testing.T) {
	dir := t.TempDir()
	writeDeb(t, dir, "1.2.0.deb", "Package: com.example.pkg\nName: Example\nVersion: 1.2.0\nFilename: stale\n")
	writeDeb(t, dir, "1.3.0.deb", "Package: com.example.pkg\nName: Example\nVersion: 1.3.0\n")
	rels := map[uint64]*model.Release{
		1: {ID: 1, Version: "1.1.0", Package: "com.example.pkg", FileName: "1.1.0.deb", Status: model.ReleasePublished, InRepo: true},
		2: {ID: 2, Version: "1.2.0", Package: "com.example.pkg", FileName: "1.2.0.deb", Status: model.ReleasePublished, InRepo: true},
		3: {ID: 3, Version: "1.3.0", Package: "com.example.pkg", FileName: "1.3.0.deb", Status: model.ReleasePublished}, // 上传时没选同步
	}
	set := &repository.Set{Releases: repoReleases{m: rels}}
	s := NewRepoService(set, nil, nil, nil, zap.NewNop(), dir, true, "")

	idx, err := s.Index(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// 1.3.0 没同步到源；1.2.0 是同步过的版本里最新的。
	if idx.Release == nil || idx.Release.Version != "1.2.0" {
		t.Fatalf("release %+v", idx.Release)
	}
	pk := string(idx.Packages)
	data, _ := os.ReadFile(filepath.Join(dir, "1.2.0.deb"))
	sum := sha256.Sum256(data)
	for _, want := range []string{"Package: com.example.pkg", "Version: 1.2.0", "Filename: debs/1.2.0.deb",
		fmt.Sprintf("Size: %d", len(data)), "SHA256: " + hex.EncodeToString(sum[:])} {
		if !strings.Contains(pk, want) {
			t.Errorf("Packages missing %q:\n%s", want, pk)
		}
	}
	if strings.Contains(pk, "Filename: stale") {
		t.Error("control 自带的 Filename 应被替换")
	}
	pkSum := sha256.Sum256(idx.Packages)
	if !strings.Contains(string(idx.ReleaseTxt), hex.EncodeToString(pkSum[:])) {
		t.Error("Release 里应有 Packages 的 SHA256")
	}
	if !strings.Contains(string(idx.ReleaseTxt), "Date: ") || !strings.Contains(string(idx.ReleaseTxt), " UTC\n") {
		t.Error("Release 应有 Date（UTC），否则 APT 警告")
	}
	if !strings.Contains(string(idx.ReleaseTxt), "Label: ResidentKit") {
		t.Error("默认源名称应为 ResidentKit")
	}
}

func TestRepoIndexEmptyWithoutSyncedRelease(t *testing.T) {
	set := &repository.Set{Releases: repoReleases{m: map[uint64]*model.Release{
		1: {ID: 1, Version: "1.0.0", Status: model.ReleaseWithdrawn, InRepo: true}, // 已撤回的不提供
	}}}
	s := NewRepoService(set, nil, nil, nil, zap.NewNop(), t.TempDir(), true, "Mine")
	idx, err := s.Index(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if idx.Release != nil || len(idx.Packages) != 0 || len(idx.PackagesGz) == 0 {
		t.Fatalf("got %+v", idx)
	}
}

func TestRepoLinkStatus(t *testing.T) {
	now := time.Now().UTC()
	past, future := now.Add(-time.Hour), now.Add(time.Hour)
	cases := []struct {
		l    model.RepoLink
		want string
	}{
		{model.RepoLink{}, "active"},
		{model.RepoLink{ExpiresAt: &future, MaxDownloads: 3, Downloads: 2}, "active"},
		{model.RepoLink{ExpiresAt: &past}, "expired"},
		{model.RepoLink{MaxDownloads: 1, Downloads: 1}, "exhausted"},
		{model.RepoLink{RevokedAt: &past, ExpiresAt: &past}, "revoked"},
	}
	for _, c := range cases {
		if got := dto.RepoLinkStatus(&c.l, now); got != c.want {
			t.Errorf("%+v: got %s want %s", c.l, got, c.want)
		}
	}
}

func TestEnrollTicketStatus(t *testing.T) {
	now := time.Now().UTC()
	past, future := now.Add(-time.Minute), now.Add(time.Minute)
	cases := []struct {
		tk   model.EnrollTicket
		want string
	}{
		{model.EnrollTicket{ExpiresAt: future, MaxUses: 1}, "active"},
		{model.EnrollTicket{ExpiresAt: future}, "active"}, // 不限次数
		{model.EnrollTicket{ExpiresAt: past, MaxUses: 1}, "expired"},
		{model.EnrollTicket{ExpiresAt: future, MaxUses: 1, Uses: 1}, "exhausted"},
		{model.EnrollTicket{ExpiresAt: future, RevokedAt: &past}, "revoked"},
	}
	for _, c := range cases {
		if got := dto.EnrollTicketStatus(&c.tk, now); got != c.want {
			t.Errorf("%+v: got %s want %s", c.tk, got, c.want)
		}
	}
}

type repoLinksByHash struct {
	repository.RepoLinkRepository
	hash string
}

func (f repoLinksByHash) FindByHash(_ context.Context, h string) (*model.RepoLink, error) {
	if h == f.hash {
		return &model.RepoLink{ID: 7}, nil
	}
	return nil, repository.ErrNotFound
}

// Sileo 经 sileo://source/ 添加源会把地址转成小写：令牌必须全小写，改了大小写也要能认。
func TestRepoLinkTokenSurvivesLowercasing(t *testing.T) {
	tok := repoLinkToken()
	if tok != strings.ToLower(tok) || strings.Contains(tok, "_") || !strings.HasPrefix(tok, repoLinkPrefix) || len(tok) != len(repoLinkPrefix)+64 {
		t.Fatalf("token %q", tok)
	}
	set := &repository.Set{RepoLinks: repoLinksByHash{hash: secure.TokenHash(tok)}}
	s := NewRepoService(set, nil, nil, nil, zap.NewNop(), t.TempDir(), true, "")
	for _, in := range []string{tok, strings.ToUpper(tok)} {
		if l, err := s.Resolve(context.Background(), in); err != nil || l.ID != 7 {
			t.Fatalf("%q: %v %v", in[:12], l, err)
		}
	}
}
