// Package migrate 在启动时执行 goose 迁移。
package migrate

import (
	"context"
	"database/sql"
	"time"

	"github.com/Mythicove/mythic-mirror/migrations"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// advisoryLockKey 是迁移串行化用的 PG advisory lock 键（跨服务唯一即可，取固定常量）。
// 多副本同时启动时，只有持锁者跑迁移，其余等待，避免 goose 并发建表冲突。
const advisoryLockKey int64 = 0x6d797468 // "myth"

// Run 对 dsn 指向的数据库执行迁移（advisory-lock 串行化，多副本安全）。
func Run(dsn string) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// pg_advisory_lock 会阻塞直到取得锁；用独立连接持有到迁移结束。
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", advisoryLockKey); err != nil {
		return err
	}
	//nolint:errcheck // 连接关闭时锁自动释放；显式解锁仅为尽早释放。
	defer conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", advisoryLockKey)

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	return goose.Up(db, ".")
}

// WithLock 在与迁移相同的 PG advisory lock 内执行 fn（多副本串行）。
// 供启动期需与迁移互斥的一次性操作（如 configseed 的 env→DB 播种）复用同一把锁。
func WithLock(dsn string, fn func(ctx context.Context) error) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", advisoryLockKey); err != nil {
		return err
	}
	//nolint:errcheck // 连接关闭时锁自动释放；显式解锁仅为尽早释放。
	defer conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", advisoryLockKey)

	return fn(ctx)
}
