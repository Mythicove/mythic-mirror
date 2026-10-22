// Package migrate 在启动时执行 goose 迁移。
package migrate

import (
	"database/sql"

	"github.com/Mythicove/mythic-mirror/migrations"
	"github.com/pressly/goose/v3"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// Run 对 dsn 指向的数据库执行迁移。
func Run(dsn string) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer db.Close()

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	return goose.Up(db, ".")
}
