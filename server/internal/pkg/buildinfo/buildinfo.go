// Package buildinfo 构建信息，版本号由 -ldflags "-X" 注入。
package buildinfo

import "time"

// Version 服务端版本。
var Version = "dev"

// StartedAt 进程启动时间。
var StartedAt = time.Now().UTC()
