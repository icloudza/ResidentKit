// Package app 装配依赖并管理服务生命周期：数据库、service、设备网关、控制台、定时任务。
package app

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/icloudza/ResidentKit/server/internal/config"
	"github.com/icloudza/ResidentKit/server/internal/dto"
	"github.com/icloudza/ResidentKit/server/internal/gateway"
	"github.com/icloudza/ResidentKit/server/internal/handler"
	"github.com/icloudza/ResidentKit/server/internal/handler/open"
	repohandler "github.com/icloudza/ResidentKit/server/internal/handler/repo"
	"github.com/icloudza/ResidentKit/server/internal/pkg/secure"
	"github.com/icloudza/ResidentKit/server/internal/repository"
	"github.com/icloudza/ResidentKit/server/internal/service"
	"github.com/icloudza/ResidentKit/server/web"
)

// App 一个完整的服务实例。
type App struct {
	Cfg  *config.Config
	Log  *zap.Logger
	DB   *gorm.DB
	Repo *repository.Set

	Bus         *service.Bus
	Audit       *service.AuditService
	Auth        *service.AuthService
	Setup       *service.SetupService
	Devices     *service.DeviceService
	Events      *service.EventService
	Invocations *service.InvocationService
	Hub         *gateway.Hub
	Dashboard   *service.DashboardService
	OTA         *service.OTAService
	Config      *service.ConfigService
	Enroll      *service.EnrollService
	APIKeys     *service.APIKeyService
	Source      *service.RepoService // 软件源（/repo/）
	OpenAPI     http.Handler
}

// New 连接数据库并装配全部 service（不启动监听，命令行工具也复用它）。
func New(cfg *config.Config, log *zap.Logger) (*App, error) {
	db, err := OpenDB(cfg)
	if err != nil {
		return nil, err
	}
	jwtKey, totpKey := cfg.Auth.JWTKey, cfg.Auth.TOTPKey
	if len(jwtKey) < 32 || len(totpKey) < 32 {
		return nil, errors.New("auth.jwt_key / auth.totp_key 至少 32 个字符（可用 openssl rand -hex 32 生成）")
	}
	sealer, err := secure.NewSealer(totpKey)
	if err != nil {
		return nil, err
	}

	a := &App{Cfg: cfg, Log: log, DB: db, Repo: repository.NewSet(db), Bus: service.NewBus()}
	a.Audit = service.NewAuditService(a.Repo.Audit, log)
	a.Auth = service.NewAuthService(a.Repo, a.Audit, sealer, service.AuthOptions{
		JWTKey: []byte(jwtKey), AccessTTL: cfg.Auth.AccessTTL, RefreshTTL: cfg.Auth.RefreshTTL,
		MaxFailures: cfg.Auth.MaxFailures, LockFor: cfg.Auth.LockFor,
	})
	a.Setup = service.NewSetupService(a.Repo, a.Auth, a.Audit, log)
	a.Devices = service.NewDeviceService(a.Repo, a.Audit, a.Bus)
	a.Events = service.NewEventService(a.Repo, a.Bus)
	a.Invocations = service.NewInvocationService(a.Repo, a.Devices, a.Audit, a.Bus, log,
		cfg.Invoke.DefaultTTL, cfg.Invoke.WaitMax)
	a.OTA, err = service.NewOTAService(a.Repo, a.Devices, a.Invocations, a.Auth, a.Audit, a.Bus, log,
		cfg.Release.PublicKey, cfg.Release.Dir)
	if err != nil {
		return nil, err
	}
	a.Events.AddHook(a.OTA.OnEvent)
	a.Config = service.NewConfigService(a.Repo, a.Devices, a.Invocations, a.Audit, a.Bus, log)
	a.Events.AddHook(a.Config.OnEvent)
	a.Enroll = service.NewEnrollService(a.Repo, a.Devices, a.Audit, a.Bus, log)
	a.APIKeys = service.NewAPIKeyService(a.Repo, a.Auth, a.Audit, log)
	a.Hub = gateway.NewHub(cfg.Gateway, gateway.Services{
		Devices: a.Devices, Events: a.Events, Invocations: a.Invocations, OTA: a.OTA, Config: a.Config, Enroll: a.Enroll}, log)
	a.Invocations.SetSender(a.Hub)
	a.OpenAPI = open.NewRouter(open.Deps{
		Log: log.Named("open"), Keys: a.APIKeys, Devices: a.Devices, Invocations: a.Invocations, Events: a.Events,
		OnAuthFail: func(ip string) { a.Hub.DenyIP(ip, "open api: bad key") },
	})
	a.Hub.SetOpenAPI(a.OpenAPI)
	a.Source = service.NewRepoService(a.Repo, a.Auth, a.Audit, sealer, log, cfg.Release.Dir, cfg.Repo.Enabled, cfg.Repo.Label)
	if cfg.Repo.Enabled {
		icon, _ := fs.ReadFile(web.FS(), "logo.png")
		a.Hub.SetRepo(repohandler.NewHandler(repohandler.Deps{
			Service: a.Source, Log: log.Named("repo"), Icon: icon,
			OnUnknown: func(ip string) { a.Hub.DenyIP(ip, "repo: unknown link") },
		}))
	}
	a.Dashboard = service.NewDashboardService(a.Repo, a.Hub)
	return a, nil
}

// OpenDB 打开 MySQL 连接池。
func OpenDB(cfg *config.Config) (*gorm.DB, error) {
	dsn := cfg.Database.DSN()
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		// 只记慢查询和真正的错误；"未找到" 是正常结果，不记。
		Logger: gormlogger.New(log.New(os.Stderr, "", log.LstdFlags), gormlogger.Config{
			SlowThreshold: cfg.Database.SlowThreshold, LogLevel: gormlogger.Warn,
			IgnoreRecordNotFoundError: true, Colorful: false, ParameterizedQueries: true,
		}),
		NowFunc: func() time.Time { return time.Now().UTC() },
		// 表结构由迁移管理，禁止 GORM 自动建外键等。
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		return nil, fmt.Errorf("连接数据库失败: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(cfg.Database.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.Database.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(cfg.Database.ConnMaxLifetime)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("数据库不可用: %w", err)
	}
	return db, nil
}

// Run 启动设备网关、控制台与定时任务，直到 ctx 取消后优雅退出。
func (a *App) Run(ctx context.Context) error {
	if err := a.Devices.ResetPresence(ctx); err != nil {
		return err
	}
	if err := a.Invocations.Recover(ctx); err != nil {
		return err
	}
	if err := a.Setup.Prepare(ctx); err != nil {
		return err
	}

	tlsCfg, cert, err := gatewayTLS(a.Cfg)
	if err != nil {
		return err
	}
	if tlsCfg == nil && !a.Cfg.GatewayIsLoopback() {
		a.Log.Warn("gateway listens on a public address without TLS: device tokens travel in clear text; "+
			"put Nginx (HTTPS) in front and listen on 127.0.0.1, or set gateway.tls_cert / tls_key",
			zap.String("addr", a.Cfg.Gateway.Listen))
	}

	console := handler.NewRouter(handler.Deps{
		Config: a.Cfg, Log: a.Log.Named("console"), Auth: a.Auth, Setup: a.Setup, Devices: a.Devices, Events: a.Events,
		Invocations: a.Invocations, Audit: a.Audit, Bus: a.Bus, Dashboard: a.Dashboard, OTA: a.OTA, DevConfig: a.Config, Enroll: a.Enroll, APIKeys: a.APIKeys, APIStats: a.Repo.APIStats, Repo: a.Source,
		OpenAPI: a.OpenAPI, Catalog: a.commandCatalog, OpenSpec: open.BuildSpec,
		OpenInfo: a.openInfo(cert), Web: web.FS(),
	})
	// 一个端口：设备连接、接入申请、OTA 下载、开放 API 交给网关，其余是控制台。
	srv := &http.Server{
		Addr: a.Cfg.Gateway.Listen, Handler: a.route(console), TLSConfig: tlsCfg,
		ReadHeaderTimeout: a.Cfg.Gateway.HandshakeTimeout, IdleTimeout: 120 * time.Second, MaxHeaderBytes: 16 << 10,
		ErrorLog: zap.NewStdLog(a.Log.Named("http")),
	}

	jobs := a.cron()
	jobs.Start()

	errc := make(chan error, 1)
	go func() {
		a.Log.Info("listening", zap.String("addr", srv.Addr), zap.Bool("tls", tlsCfg != nil))
		if tlsCfg != nil {
			errc <- srv.ListenAndServeTLS("", "")
		} else {
			errc <- srv.ListenAndServe()
		}
	}()

	select {
	case <-ctx.Done():
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			a.Log.Error("server stopped", zap.Error(err))
		}
	}

	a.Log.Info("shutting down")
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	<-jobs.Stop().Done()
	a.Hub.Shutdown(shutdown)
	_ = srv.Shutdown(shutdown)
	if sqlDB, err := a.DB.DB(); err == nil {
		_ = sqlDB.Close()
	}
	return nil
}

// cron 定时任务：过期离线指令、清理旧数据、整理封禁表。
func (a *App) cron() *cron.Cron {
	c := cron.New()
	run := func(name string, f func(ctx context.Context)) func() {
		return func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			defer func() {
				if r := recover(); r != nil {
					a.Log.Error("cron panic", zap.String("job", name), zap.Any("err", r))
				}
			}()
			f(ctx)
		}
	}
	_, _ = c.AddFunc("@every 30s", run("expire", a.Invocations.ExpirePending))
	_, _ = c.AddFunc("@every 30s", run("ota", a.OTA.Tick))
	_, _ = c.AddFunc("@every 30s", run("enroll", a.Enroll.Expire))
	_, _ = c.AddFunc("@every 10m", run("housekeep", func(context.Context) { a.Hub.Housekeep(); a.Enroll.GC() }))
	_, _ = c.AddFunc("15 3 * * *", run("retention", a.retention))
	return c
}

// retention 按保留期删除旧事件、指令与审计日志，分批避免长事务。
func (a *App) retention(ctx context.Context) {
	now := time.Now().UTC()
	for {
		n, err := a.Repo.Events.DeleteBefore(ctx, now.Add(-a.Cfg.Retain.Events))
		if err != nil || n == 0 {
			break
		}
	}
	for {
		n, err := a.Repo.Invocations.DeleteBefore(ctx, now.Add(-a.Cfg.Retain.Invocations))
		if err != nil || n == 0 {
			break
		}
	}
	if a.Cfg.Retain.Audit > 0 {
		for {
			n, err := a.Repo.Audit.DeleteBefore(ctx, now.Add(-a.Cfg.Retain.Audit))
			if err != nil || n == 0 {
				break
			}
		}
	}
	_, _ = a.Repo.RefreshTokens.DeleteExpired(ctx, now)
	a.OTA.PruneFiles(ctx)
	a.Log.Info("retention done")
}

// commandCatalog 全部设备的命令目录（控制台「接口文档」与规范下载用）。
func (a *App) commandCatalog(ctx context.Context) ([]dto.CommandSpec, error) {
	devs, err := a.Devices.Models(ctx)
	if err != nil {
		return nil, err
	}
	return service.CommandCatalog(devs), nil
}

// route 按路径分发：网关路径交给 Hub，其余交给控制台。
func (a *App) route(console http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p := r.URL.Path; p == a.Cfg.Gateway.Path || p == gateway.EnrollPath ||
			strings.HasPrefix(p, gateway.OTAPrefix) || strings.HasPrefix(p, "/open/") || strings.HasPrefix(p, "/repo/") {
			a.Hub.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			// 有请求体的才限时；GET 不限，否则实时推送（SSE）会被掐断。
			_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(2 * time.Minute))
		}
		console.ServeHTTP(w, r)
	})
}

// openInfo 控制台「开放 API」页显示的地址与证书指纹。
func (a *App) openInfo(cert tls.Certificate) dto.OpenAPIInfo {
	info := dto.OpenAPIInfo{}
	if u := strings.TrimRight(a.Cfg.Gateway.PublicURL, "/"); u != "" {
		info.BaseURL = u + "/open/v1"
	}
	if len(cert.Certificate) > 0 {
		sum := sha256.Sum256(cert.Certificate[0])
		info.CertPin = hex.EncodeToString(sum[:])
		if c, err := x509.ParseCertificate(cert.Certificate[0]); err == nil {
			spki := sha256.Sum256(c.RawSubjectPublicKeyInfo)
			info.SPKIPin = base64.StdEncoding.EncodeToString(spki[:])
		}
	}
	return info
}
