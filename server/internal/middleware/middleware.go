// Package middleware Gin 中间件：请求 ID 与日志、异常恢复、安全响应头、来源校验、限流、认证。
package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ulule/limiter/v3"
	"github.com/ulule/limiter/v3/drivers/store/memory"
	"go.uber.org/zap"

	"github.com/icloudza/ResidentKit/server/internal/pkg/errcode"
	"github.com/icloudza/ResidentKit/server/internal/pkg/response"
	"github.com/icloudza/ResidentKit/server/internal/service"
)

const (
	ctxLogger = "logger"
	ctxActor  = "actor"
)

// RequestLog 为每个请求生成 ID，注入带 ID 的 logger，并记录访问日志。
func RequestLog(log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		b := make([]byte, 8)
		_, _ = rand.Read(b)
		id := hex.EncodeToString(b)
		l := log.With(zap.String("req", id))
		c.Set(ctxLogger, l)
		c.Header("X-Request-ID", id)
		start := time.Now()
		c.Next()
		if c.Request.URL.Path == "/api/v1/stream" {
			return // 长连接，不计耗时
		}
		l.Info("http", zap.String("method", c.Request.Method), zap.String("path", c.FullPath()),
			zap.Int("status", c.Writer.Status()), zap.Duration("took", time.Since(start)),
			zap.String("ip", c.ClientIP()))
	}
}

// Recover 捕获 panic，返回统一的 500 响应。
func Recover() gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, err any) {
		if l, ok := c.Get(ctxLogger); ok {
			l.(*zap.Logger).Error("panic", zap.Any("err", err), zap.Stack("stack"))
		}
		response.Error(c, errcode.Internal)
	})
}

// SecureHeaders 严格 CSP、禁止嵌入、禁止 MIME 嗅探；启用 TLS 时加 HSTS。
func SecureHeaders(tls bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		script := "'self'"
		if strings.HasPrefix(c.Request.URL.Path, "/swagger/") {
			script = "'self' 'unsafe-inline'" // Swagger UI 自带内联脚本
		}
		h.Set("Content-Security-Policy", "default-src 'self'; script-src "+script+"; style-src 'self' 'unsafe-inline'; "+
			"img-src 'self' data:; connect-src 'self'; font-src 'self'; object-src 'none'; "+
			"frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		if tls {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			h.Set("Cache-Control", "no-store")
		}
		c.Next()
	}
}

// SameOrigin 非 GET 请求必须同源（Origin 与 Host 一致或在白名单中），防 CSRF。
func SameOrigin(trusted []string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead {
			c.Next()
			return
		}
		origin := c.GetHeader("Origin")
		if origin == "" {
			c.Next() // 非浏览器客户端（curl 等）不带 Origin，仍需令牌认证
			return
		}
		u, err := url.Parse(origin)
		if err != nil || (u.Host != c.Request.Host && !slices.Contains(trusted, origin)) {
			response.Error(c, errcode.Forbidden.WithMessage("跨站请求被拒绝"))
			return
		}
		c.Next()
	}
}

// BodyLimit 限制请求体大小；large 为个别路由（gin 路由模板）单独放宽的上限。
func BodyLimit(n int64, large map[string]int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		limit := n
		if v, ok := large[c.Request.Method+" "+c.FullPath()]; ok {
			limit = v
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		c.Next()
	}
}

// RateLimit 按客户端 IP 限流，perMinute 为每分钟请求数。
func RateLimit(perMinute int64) gin.HandlerFunc {
	l := limiter.New(memory.NewStore(), limiter.Rate{Period: time.Minute, Limit: perMinute})
	return func(c *gin.Context) {
		ctx, err := l.Get(c, c.ClientIP())
		if err == nil && ctx.Reached {
			response.Error(c, errcode.TooManyRequests)
			return
		}
		c.Next()
	}
}

// Auth 校验 Authorization: Bearer 访问令牌，并注入操作者。
func Auth(auth *service.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, ok := strings.CutPrefix(c.GetHeader("Authorization"), "Bearer ")
		if !ok {
			response.Error(c, errcode.Unauthorized)
			return
		}
		claims, err := auth.ParseAccessToken(token)
		if err != nil {
			response.Error(c, err)
			return
		}
		id, err := strconv.ParseUint(claims.Subject, 10, 64)
		if err != nil || id == 0 {
			response.Error(c, errcode.Unauthorized)
			return
		}
		c.Set(ctxActor, service.Actor{AdminID: id, Username: claims.Username, IP: c.ClientIP()})
		c.Next()
	}
}

// ActorFrom 取当前操作者；未认证的路由返回仅含 IP 的 Actor。
func ActorFrom(c *gin.Context) service.Actor {
	if v, ok := c.Get(ctxActor); ok {
		return v.(service.Actor)
	}
	return service.Actor{IP: c.ClientIP()}
}
