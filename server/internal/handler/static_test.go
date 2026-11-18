package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestServeStatic(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html":       {Data: []byte("<html>")},
		"index.html.br":    {Data: []byte("BR-INDEX")},
		"assets/a.js":      {Data: []byte("raw-js")},
		"assets/a.js.br":   {Data: []byte("BR-JS")},
		"assets/a.js.gz":   {Data: []byte("GZ-JS")},
		"assets/b.css":     {Data: []byte("raw-css")},
		"assets/dir/x.txt": {Data: []byte("x")},
	}
	cases := []struct {
		name, accept, body, enc, ctype string
		ok                             bool
	}{
		{"assets/a.js", "gzip, deflate, br", "BR-JS", "br", "text/javascript; charset=utf-8", true},
		{"assets/a.js", "gzip", "GZ-JS", "gzip", "text/javascript; charset=utf-8", true},
		{"assets/a.js", "", "raw-js", "", "text/javascript; charset=utf-8", true},
		{"assets/b.css", "br", "raw-css", "", "text/css; charset=utf-8", true},
		{"index.html", "br", "BR-INDEX", "br", "text/html; charset=utf-8", true},
		{"assets/dir", "", "", "", "", false},
		{"missing.js", "br", "", "", "", false},
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/"+c.name, nil)
		if c.accept != "" {
			r.Header.Set("Accept-Encoding", c.accept)
		}
		ok := serveStatic(w, r, fsys, c.name)
		if ok != c.ok {
			t.Fatalf("%s %q: ok=%v", c.name, c.accept, ok)
		}
		if !ok {
			continue
		}
		if w.Body.String() != c.body || w.Header().Get("Content-Encoding") != c.enc || w.Header().Get("Content-Type") != c.ctype {
			t.Errorf("%s %q: body=%q enc=%q type=%q", c.name, c.accept, w.Body.String(), w.Header().Get("Content-Encoding"), w.Header().Get("Content-Type"))
		}
	}
}
