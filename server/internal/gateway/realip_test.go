package gateway

import (
	"net/http"
	"testing"
)

func TestRealIP(t *testing.T) {
	p := parseProxies([]string{"127.0.0.1", "::1", "10.0.0.0/8"})
	cases := []struct {
		remote, xff, xri, want string
	}{
		{"203.0.113.9:5000", "", "", "203.0.113.9"},
		{"203.0.113.9:5000", "1.2.3.4", "1.2.3.4", "203.0.113.9"}, // 不是反代：忽略头
		{"127.0.0.1:5000", "", "198.51.100.7", "198.51.100.7"},
		{"127.0.0.1:5000", "198.51.100.7", "", "198.51.100.7"},
		{"127.0.0.1:5000", "6.6.6.6, 198.51.100.7", "", "198.51.100.7"},  // 客户端伪造的最左项被跳过
		{"127.0.0.1:5000", "198.51.100.7, 10.1.2.3", "", "198.51.100.7"}, // 多层反代
		{"[::1]:5000", "2001:db8::1", "", "2001:db8::1"},
		{"127.0.0.1:5000", "", "", "127.0.0.1"},
	}
	for _, c := range cases {
		r := &http.Request{RemoteAddr: c.remote, Header: http.Header{}}
		if c.xff != "" {
			r.Header.Set("X-Forwarded-For", c.xff)
		}
		if c.xri != "" {
			r.Header.Set("X-Real-IP", c.xri)
		}
		if got := p.realIP(r); got != c.want {
			t.Errorf("%+v: got %s", c, got)
		}
	}
}
