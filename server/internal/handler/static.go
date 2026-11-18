package handler

import (
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"
)

// serveStatic 返回前端文件；有构建时生成的 .br / .gz 预压缩版本且客户端支持时直接返回它。
func serveStatic(w http.ResponseWriter, r *http.Request, fsys fs.FS, name string) bool {
	accept := r.Header.Get("Accept-Encoding")
	for _, enc := range [...]struct{ ext, name string }{{".br", "br"}, {".gz", "gzip"}} {
		if !strings.Contains(accept, enc.name) {
			continue
		}
		if f, err := fsys.Open(name + enc.ext); err == nil {
			defer f.Close()
			rs, ok := f.(io.ReadSeeker)
			if !ok {
				continue
			}
			h := w.Header()
			h.Set("Content-Encoding", enc.name)
			h.Add("Vary", "Accept-Encoding")
			if ct := mime.TypeByExtension(path.Ext(name)); ct != "" {
				h.Set("Content-Type", ct)
			}
			http.ServeContent(w, r, "", time.Time{}, rs)
			return true
		}
	}
	f, err := fsys.Open(name)
	if err != nil {
		return false
	}
	defer f.Close()
	if st, err := f.Stat(); err != nil || st.IsDir() {
		return false
	}
	rs, ok := f.(io.ReadSeeker)
	if !ok {
		return false
	}
	w.Header().Add("Vary", "Accept-Encoding")
	http.ServeContent(w, r, name, time.Time{}, rs)
	return true
}
