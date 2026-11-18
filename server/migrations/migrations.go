// Package migrations 嵌入版本化 SQL 迁移（golang-migrate）。
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
