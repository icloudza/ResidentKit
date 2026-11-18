// Package repo 软件源：/repo/<令牌>/ 供 Sileo 等添加为源。
//
//	Release、Packages、Packages.gz   源索引（只含已全量发布的最新版本）
//	debs/<版本>.deb                    安装包，每次完整下载占用一次下载名额
//	CydiaIcon.png                     源图标
//
// 令牌不存在计入封禁；链接已吊销 / 过期 / 用完一律 404，不说明原因
// （次数刚用完的 30 分钟内仍允许断点续传，避免下载中途断线后装不上）。
package repo

import (
	"bytes"
	"errors"
	"fmt"
	"html"
	"net"
	"net/http"
	"path"
	"strings"

	"go.uber.org/zap"

	"github.com/icloudza/ResidentKit/server/internal/model"
	"github.com/icloudza/ResidentKit/server/internal/service"
)

type Deps struct {
	Service   *service.RepoService
	Log       *zap.Logger
	OnUnknown func(ip string) // 令牌不存在：计入封禁
	Icon      []byte          // 源图标（PNG），为空则不提供
}

type handler struct{ d Deps }

func NewHandler(d Deps) http.Handler { return &handler{d: d} }

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/repo/")
	token, file, slash := strings.Cut(rest, "/")
	file = cleanFile(file)
	ip := clientIP(r)

	link, err := h.d.Service.Resolve(r.Context(), token)
	resumeOnly := errors.Is(err, service.ErrRepoLinkExhausted) // 次数刚用完：只允许续传
	if resumeOnly {
		err = nil
	}
	if err != nil {
		if errors.Is(err, service.ErrRepoLinkUnknown) && h.d.OnUnknown != nil {
			h.d.OnUnknown(ip)
		} else if !errors.Is(err, service.ErrRepoLinkInvalid) {
			h.d.Log.Error("resolve link", zap.Error(err))
		}
		http.NotFound(w, r)
		return
	}
	if !slash {
		http.Redirect(w, r, "/repo/"+token+"/", http.StatusMovedPermanently)
		return
	}
	if resumeOnly && !strings.HasPrefix(file, "debs/") { // 次数用完：链接已失效，只放行续传
		http.NotFound(w, r)
		return
	}

	switch file {
	case "":
		h.landing(w, r)
	case "Release", "Packages", "Packages.gz":
		idx, err := h.d.Service.Index(r.Context())
		if err != nil {
			h.d.Log.Error("build index", zap.Error(err))
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		body, ctype := idx.ReleaseTxt, "text/plain; charset=utf-8"
		switch file {
		case "Packages":
			body = idx.Packages
		case "Packages.gz":
			body, ctype = idx.PackagesGz, "application/gzip"
		}
		w.Header().Set("Content-Type", ctype)
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeContent(w, r, "", idx.ModTime, bytes.NewReader(body))
	case "CydiaIcon.png":
		if len(h.d.Icon) == 0 {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		_, _ = w.Write(h.d.Icon)
	default:
		name, ok := strings.CutPrefix(file, "debs/")
		if !ok || name == "" || strings.Contains(name, "/") {
			http.NotFound(w, r)
			return
		}
		h.deb(w, r, link, name, ip, resumeOnly)
	}
}

// deb 下载安装包。HEAD 和断点续传（Range 不从 0 开始）不占用下载名额，避免重试把一次性链接用完。
func (h *handler) deb(w http.ResponseWriter, r *http.Request, link *model.RepoLink, name, ip string, resumeOnly bool) {
	counted := r.Method == http.MethodGet && fromStart(r.Header.Get("Range"))
	if counted && resumeOnly {
		http.NotFound(w, r)
		return
	}
	f, rel, err := h.d.Service.OpenDeb(r.Context(), link, name, counted, service.DownloadMeta{
		IP: ip, Machine: r.Header.Get("X-Machine"), Firmware: r.Header.Get("X-Firmware"), UserAgent: r.UserAgent()})
	if err != nil {
		if !errors.Is(err, service.ErrRepoLinkInvalid) {
			h.d.Log.Debug("open deb", zap.Error(err))
		}
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/vnd.debian.binary-package")
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, name, rel.CreatedAt, f)
}

// cleanFile 规范化源内路径：APT 的平铺源（deb <地址> ./）请求的是 <地址>/./Release，
// 不规范化就会 404，Sileo 能列出包但 APT 装不上。
func cleanFile(f string) string {
	if f == "" {
		return ""
	}
	return strings.TrimPrefix(path.Clean("/"+f), "/")
}

// fromStart 没有 Range 或从第 0 字节开始：算一次完整下载。
func fromStart(rng string) bool {
	return rng == "" || strings.HasPrefix(strings.TrimSpace(rng), "bytes=0-")
}

func (h *handler) landing(w http.ResponseWriter, r *http.Request) {
	idx, err := h.d.Service.Index(r.Context())
	ver := "暂无可安装的版本"
	if err == nil && idx.Release != nil {
		ver = "当前版本 " + idx.Release.Version
	}
	scheme := "https"
	if r.TLS == nil && r.Header.Get("X-Forwarded-Proto") != "https" && !strings.HasPrefix(r.Host, "localhost") {
		scheme = "http"
	}
	url := fmt.Sprintf("%s://%s%s", scheme, r.Host, r.URL.Path)
	label := html.EscapeString(h.d.Service.Label())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	fmt.Fprintf(w, `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1"><meta name="robots" content="noindex">
<title>%[1]s 软件源</title><style>
body{font:16px/1.6 -apple-system,system-ui,sans-serif;margin:0;padding:32px 20px;background:#f5f5f7;color:#1d1d1f}
main{max-width:520px;margin:0 auto;background:#fff;border-radius:18px;padding:28px}
h1{font-size:22px;margin:0 0 4px}p{color:#6e6e73;margin:6px 0}
code{display:block;word-break:break-all;background:#f5f5f7;border-radius:10px;padding:12px;margin:16px 0;font-size:13px}
a.btn{display:block;text-align:center;background:#0071e3;color:#fff;text-decoration:none;border-radius:12px;padding:12px;font-weight:600}
@media(prefers-color-scheme:dark){body{background:#000;color:#f5f5f7}main{background:#1c1c1e}code{background:#2c2c2e}}
</style></head><body><main><h1>%[1]s 软件源</h1><p>%[2]s</p>
<code>%[3]s</code><a class="btn" href="sileo://source/%[3]s">在 Sileo 中添加</a>
<p>或在 Sileo「源」页点右上角「+」，粘贴上面的地址。</p></main></body></html>`,
		label, html.EscapeString(ver), html.EscapeString(url))
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
