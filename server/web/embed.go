// Package web 嵌入打包后的控制台前端（web/dist，由 `make web` 生成）。
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// FS 前端静态文件根目录。
func FS() fs.FS {
	sub, _ := fs.Sub(dist, "dist")
	return sub
}
