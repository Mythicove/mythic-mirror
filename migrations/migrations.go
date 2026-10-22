// Package migrations 内嵌 goose 迁移文件。
package migrations

import "embed"

// FS 是迁移文件的只读文件系统。
//
//go:embed *.sql
var FS embed.FS
