package gateway

import (
	"net"
	"net/http"
	"strings"
)

// proxies 受信任的反代地址。来自它们的请求按 X-Real-IP / X-Forwarded-For 取客户端真实 IP；
// 其他来源一律用 TCP 对端地址，伪造的头不起作用。
type proxies []*net.IPNet

func parseProxies(list []string) proxies {
	var out proxies
	for _, s := range list {
		if _, n, err := net.ParseCIDR(s); err == nil {
			out = append(out, n)
		} else if ip := net.ParseIP(s); ip != nil {
			bits := 128
			if ip.To4() != nil {
				ip, bits = ip.To4(), 32
			}
			out = append(out, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
		}
	}
	return out
}

func (p proxies) trusted(ip net.IP) bool {
	for _, n := range p {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// realIP 客户端真实 IP。X-Forwarded-For 从右往左跳过受信任的反代，取第一个不是反代的地址。
func (p proxies) realIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer := net.ParseIP(host)
	if peer == nil || !p.trusted(peer) {
		return host
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		for i := len(parts) - 1; i >= 0; i-- {
			ip := net.ParseIP(strings.TrimSpace(parts[i]))
			if ip == nil {
				break
			}
			if !p.trusted(ip) || i == 0 {
				return ip.String()
			}
		}
	}
	if ip := net.ParseIP(strings.TrimSpace(r.Header.Get("X-Real-IP"))); ip != nil {
		return ip.String()
	}
	return host
}
