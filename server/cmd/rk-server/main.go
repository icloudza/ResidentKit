// Command rk-server 是 ResidentKit 服务端：设备网关 + 控制台 + 开放 API。
//
// 直接运行即可，读取程序同目录的 config.yaml；启动时自动建表、升级表结构。
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	_ "github.com/icloudza/ResidentKit/server/docs" // Swagger 文档
	"github.com/icloudza/ResidentKit/server/internal/app"
	"github.com/icloudza/ResidentKit/server/internal/config"
	"github.com/icloudza/ResidentKit/server/internal/pkg/logger"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "启动失败:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(configPath())
	if err != nil {
		return err
	}
	if _, err := app.MigrateUp(cfg.Database.DSN()); err != nil {
		return fmt.Errorf("数据库迁移失败: %w", err)
	}
	log, err := logger.New(cfg.Log.Level, cfg.Log.Format)
	if err != nil {
		return err
	}
	defer func() { _ = log.Sync() }()
	a, err := app.New(cfg, log)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return a.Run(ctx)
}

// configPath 程序同目录的 config.yaml；开发时（go run）用当前目录的。
func configPath() string {
	if exe, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(exe), "config.yaml")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "config.yaml"
}
