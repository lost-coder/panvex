// Package sqlite hosts the SQLite-backed storage.Store implementation.
// This file owns schema management — it delegates entirely to goose, which
// discovers versioned .sql migrations from an embedded FS and records applied
// versions in the goose_db_version table.
//
// History: the migration tree was squashed into a single 0001_init.sql in
// P9 (2026-07). Databases created before the squash carry goose versions
// 1..58 and see 0001 as already superseded; new migrations therefore MUST
// use versions >= 0059 — the parity lint in storage/migrate enforces this.
// The former fresh-install baseline fast-path (applyBaselineIfFresh) was
// deleted with the squash: 0001_init.sql IS the baseline now.
package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"sync"

	sqlitemigrations "github.com/lost-coder/panvex/db/migrations/sqlite"
	"github.com/pressly/goose/v3"
)

// gooseMu serialises access to the package-level goose global state
// (SetBaseFS / SetDialect are global in goose v3). Migrate and Status must
// never race each other across concurrent openings.
var gooseMu sync.Mutex

// Migrate brings the database schema up to the latest embedded migration.
// Safe to call repeatedly: goose skips versions already recorded in
// goose_db_version.
func Migrate(db *sql.DB) error {
	return MigrateContext(context.Background(), db)
}

// MigrateContext is the context-aware variant of Migrate.
//
// SQLite intentionally skips the migrateguard.CheckAll step: the
// destructive registry is empty since the P9 squash. If a future
// destructive migration applies to SQLite as well, wire CheckAll here
// the same way postgres/migrate.go does.
func MigrateContext(ctx context.Context, db *sql.DB) error {
	gooseMu.Lock()
	defer gooseMu.Unlock()

	if err := configureGoose(); err != nil {
		return err
	}
	if err := goose.UpContext(ctx, db, "."); err != nil {
		return fmt.Errorf("sqlite: goose up: %w", err)
	}
	return nil
}

// Status writes the applied/pending migration list to stdout via goose's
// default logger. The operator invokes this through the
// `migrate-schema status` subcommand on the control-plane binary.
func Status(ctx context.Context, db *sql.DB) error {
	gooseMu.Lock()
	defer gooseMu.Unlock()

	if err := configureGoose(); err != nil {
		return err
	}
	if err := goose.StatusContext(ctx, db, "."); err != nil {
		return fmt.Errorf("sqlite: goose status: %w", err)
	}
	return nil
}

func configureGoose() error {
	goose.SetBaseFS(sqlitemigrations.FS)
	// modernc.org/sqlite registers its driver under the name "sqlite", but
	// goose's dialect identifier for SQLite is "sqlite3". The dialect string
	// selects SQL syntax handling inside goose; it does not need to match the
	// database/sql driver name.
	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("sqlite: goose set dialect: %w", err)
	}
	return nil
}
