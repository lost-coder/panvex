// Package sqlite hosts the SQLite-backed storage.Store implementation.
// This file owns schema management — it delegates entirely to goose, which
// discovers versioned .sql migrations from an embedded FS and records applied
// versions in the goose_db_version table. Historically this package contained
// a hand-rolled Migrate() with a single big initialSchema string plus a long
// tail of ensureXxxColumn / ensureXxxTable helpers that papered over SQLite's
// lack of "ALTER TABLE ... IF NOT EXISTS"; that approach left no audit trail
// of which migrations had run (see DF-20 / M-F8 in the security review).
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
