// Package handler 组装控制台的路由：REST API、SSE、Swagger 与前端静态文件。
package handler

import (
	"context"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"go.uber.org/zap"

	"github.com/icloudza/ResidentKit/server/internal/config"
	"github.com/icloudza/ResidentKit/server/internal/dto"
	v1 "github.com/icloudza/ResidentKit/server/internal/handler/v1"
	"github.com/icloudza/ResidentKit/server/internal/middleware"
	"github.com/icloudza/ResidentKit/server/internal/pkg/errcode"
	"github.com/icloudza/ResidentKit/server/internal/pkg/response"
	"github.com/icloudza/ResidentKit/server/internal/repository"
	"github.com/icloudza/ResidentKit/server/internal/service"
)

// Deps 路由依赖。
type Deps struct {
	Config      *config.Config
	Log         *zap.Logger
	Auth        *service.AuthService
	Setup       *service.SetupService
	Devices     *service.DeviceService
	Events      *service.EventService
	Invocations *service.InvocationService
	Audit       *service.AuditService
	Bus         *service.Bus
	Dashboard   *service.DashboardService
	OTA         *service.OTAService
	DevConfig   *service.ConfigService
	Enroll      *service.EnrollService
	APIKeys     *service.APIKeyService
	APIStats    repository.APIStatsRepository
	Repo        *service.RepoService
	OpenInfo    dto.OpenAPIInfo
	OpenAPI     http.Handler // 开放 API 处理器（控制台「在线调试」用）
	Catalog     func(ctx context.Context) ([]dto.CommandSpec, error)
	OpenSpec    func([]dto.CommandSpec) []byte
	Web         fs.FS
}

func NewRouter(d Deps) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.HandleMethodNotAllowed = true
	// 只信任配置的反代（默认本机）转发的 X-Forwarded-For / X-Real-IP，
	// 控制台经反代访问时登录限流和审计记录的是真实 IP；其他来源直接取 TCP 对端地址。
	_ = r.SetTrustedProxies(d.Config.Gateway.TrustedProxies)

	r.Use(middleware.RequestLog(d.Log), middleware.Recover(),
		middleware.SecureHeaders(d.Config.HTTPS()), middleware.BodyLimit(1<<20, map[string]int64{"POST /api/v1/releases": v1.UploadMaxBytes}))

	r.GET("/healthz", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	authH := v1.NewAuthHandler(d.Auth, d.Setup, d.Config.HTTPS())
	dash := v1.NewDashboardHandler(d.Dashboard)
	dev := v1.NewDeviceHandler(d.Devices, d.Invocations, d.DevConfig)
	inv := v1.NewInvocationHandler(d.Invocations)
	ev := v1.NewEventHandler(d.Events)
	tok := v1.NewTokenHandler(d.Devices)
	aud := v1.NewAuditHandler(d.Audit)
	stream := v1.NewStreamHandler(d.Bus)
	rel := v1.NewReleaseHandler(d.OTA, d.Config.Repo.Enabled)
	pin := ""
	if d.Config.GatewayTLS() {
		pin = d.OpenInfo.CertPin // 网关自己做 TLS（可能是自签证书）：二维码带上证书指纹
	}
	enr := v1.NewEnrollHandler(d.Enroll, d.Config, pin)
	keys := v1.NewAPIKeyHandler(d.APIKeys, d.APIStats, d.OpenInfo, d.OpenAPI, d.Catalog, d.OpenSpec)

	api := r.Group("/api/v1", middleware.SameOrigin(d.Config.Console.TrustedOrigin))
	{
		// 免登录：登录和刷新单独限流。
		pub := api.Group("/auth", middleware.RateLimit(d.Config.Auth.LoginRatePerMin))
		pub.POST("/login", authH.Login)
		pub.POST("/refresh", authH.Refresh)

		// 首次初始化：与登录共用限流。
		setup := api.Group("/setup", middleware.RateLimit(d.Config.Auth.LoginRatePerMin))
		setup.GET("/status", authH.SetupStatus)
		setup.POST("", authH.Setup)

		priv := api.Group("", middleware.RateLimit(d.Config.Auth.APIRatePerMin), middleware.Auth(d.Auth))
		priv.POST("/auth/logout", authH.Logout)
		priv.GET("/auth/me", authH.Me)
		priv.POST("/auth/password", authH.ChangePassword)
		priv.PUT("/auth/username", authH.ChangeUsername)
		priv.POST("/auth/totp/setup", authH.TOTPSetup)
		priv.POST("/auth/totp/enable", authH.TOTPEnable)
		priv.POST("/auth/totp/disable", authH.TOTPDisable)

		priv.GET("/dashboard/summary", dash.Summary)
		priv.GET("/stream", stream.Stream)

		priv.GET("/devices", dev.List)
		priv.GET("/devices/:id", dev.Get)
		priv.PATCH("/devices/:id", dev.Update)
		priv.DELETE("/devices/:id", dev.Delete)
		priv.GET("/devices/:id/commands", dev.Commands)
		priv.POST("/devices/:id/invocations", dev.Invoke)
		priv.GET("/devices/:id/config", dev.GetConfig)
		priv.GET("/devices/:id/activity", dash.DeviceActivity)
		priv.PUT("/devices/:id/config", dev.UpdateConfig)

		priv.GET("/invocations", inv.List)
		priv.GET("/invocations/:id", inv.Get)
		priv.GET("/events", ev.List)
		priv.GET("/events/types", dash.EventTypes)

		priv.GET("/tokens", tok.List)
		priv.POST("/tokens/:id/revoke", tok.Revoke)
		priv.DELETE("/tokens/:id", tok.Delete)

		priv.GET("/audit-logs", aud.List)

		priv.GET("/api-keys", keys.List)
		priv.GET("/api-keys/info", keys.Info)
		priv.GET("/api-keys/stats", keys.Stats)
		priv.GET("/api-keys/commands", keys.Commands)
		priv.GET("/api-keys/openapi.json", keys.Spec)
		priv.POST("/api-keys/try", keys.Try)
		priv.PUT("/api-keys/:id", keys.Update)
		priv.GET("/api-calls", keys.Calls)
		priv.POST("/api-keys", keys.Create)
		priv.POST("/api-keys/:id/revoke", keys.Revoke)
		priv.DELETE("/api-keys/:id", keys.Delete)

		priv.GET("/enrollments", enr.Pending)
		priv.POST("/enrollments/:id/:action", enr.Decide)
		priv.GET("/enroll-tickets", enr.Tickets)
		priv.POST("/enroll-tickets", enr.CreateTicket)
		priv.POST("/enroll-tickets/:id/revoke", enr.RevokeTicket)

		priv.GET("/releases/config", rel.Config)
		priv.GET("/releases", rel.List)
		priv.POST("/releases", rel.Upload)
		priv.GET("/releases/:id", rel.Get)
		priv.POST("/releases/:id/withdraw", rel.Withdraw)
		priv.POST("/releases/:id/repo", rel.SetRepo)
		priv.POST("/releases/:id/rollouts", rel.CreateRollout)
		priv.POST("/rollouts/:id/:action", rel.ChangeRollout)
		priv.POST("/device-updates/:id/retry", rel.Retry)
		priv.GET("/devices/:id/update", rel.DeviceUpdate)

		src := v1.NewRepoLinkHandler(d.Repo, d.Config.Gateway.PublicURL)
		priv.GET("/repo-links", src.Overview)
		priv.POST("/repo-links", src.Create)
		priv.POST("/repo-links/:id/revoke", src.Revoke)
		priv.GET("/repo-links/:id/downloads", src.Downloads)
		priv.GET("/repo-links/:id/reveal", src.Reveal)
		priv.PUT("/repo-links/:id", src.Update)
		priv.DELETE("/repo-links/:id", src.Delete)
	}

	// Swagger 文档。
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// 前端：静态文件（优先返回预压缩版本），未知路径回退到 index.html（前端路由）。
	r.NoRoute(func(c *gin.Context) {
		p := c.Request.URL.Path
		if strings.HasPrefix(p, "/api/") {
			response.Error(c, errcode.NotFound)
			return
		}
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			c.Status(http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(path.Clean(p), "/")
		if name != "" && name != "index.html" && !strings.HasSuffix(name, ".br") && !strings.HasSuffix(name, ".gz") {
			if strings.HasPrefix(name, "assets/") {
				c.Header("Cache-Control", "public, max-age=31536000, immutable")
			}
			if serveStatic(c.Writer, c.Request, d.Web, name) {
				return
			}
			c.Writer.Header().Del("Cache-Control")
		}
		c.Header("Cache-Control", "no-cache")
		if !serveStatic(c.Writer, c.Request, d.Web, "index.html") {
			c.Status(http.StatusNotFound)
		}
	})
	return r
}
