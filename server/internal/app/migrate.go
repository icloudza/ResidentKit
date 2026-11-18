package app

import (
	"database/sql"
	"errors"

	gomysql "github.com/go-sql-driver/mysql"
	"github.com/golang-migrate/migrate/v4"
	migratemysql "github.com/golang-migrate/migrate/v4/database/mysql"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/icloudza/ResidentKit/server/migrations"
)

// migrator 用 dsn 打开迁移器。用完调用 close。
func migrator(dsn string) (m *migrate.Migrate, close func(), err error) {
	if dsn == "" {
		return nil, nil, errors.New("数据库未配置")
	}
	mc, err := gomysql.ParseDSN(dsn)
	if err != nil {
		return nil, nil, err
	}
	mc.MultiStatements = true
	db, err := sql.Open("mysql", mc.FormatDSN())
	if err != nil {
		return nil, nil, err
	}
	driver, err := migratemysql.WithInstance(db, &migratemysql.Config{})
	if err != nil {
		db.Close()
		return nil, nil, err
	}
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		db.Close()
		return nil, nil, err
	}
	m, err = migrate.NewWithInstance("iofs", src, "mysql", driver)
	if err != nil {
		db.Close()
		return nil, nil, err
	}
	return m, func() { _, _ = m.Close() }, nil
}

// MigrateUp 把数据库升级到最新版本，返回当前版本。
func MigrateUp(dsn string) (uint, error) {
	m, done, err := migrator(dsn)
	if err != nil {
		return 0, err
	}
	defer done()
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return 0, err
	}
	v, _, _ := m.Version()
	return v, nil
}
