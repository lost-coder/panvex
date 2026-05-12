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
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"sync"

	sqlitebaseline "github.com/lost-coder/panvex/db/migrations/sqlite/baseline"
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
// SQLite intentionally skips the migrateguard.CheckAll step: the only
// migration currently in the destructive registry (0014) ships in a
// non-destructive ADD COLUMN + UPDATE form on this dialect (see
// db/migrations/sqlite/0014_fleet_groups_redesign.sql). If a future
// destructive migration applies to SQLite as well, wire CheckAll here
// the same way postgres/migrate.go does.
func MigrateContext(ctx context.Context, db *sql.DB) error {
	gooseMu.Lock()
	defer gooseMu.Unlock()

	if err := applyBaselineIfFresh(ctx, db); err != nil {
		return fmt.Errorf("sqlite: baseline: %w", err)
	}
	if err := configureGoose(); err != nil {
		return err
	}
	if err := goose.UpContext(ctx, db, "."); err != nil {
		return fmt.Errorf("sqlite: goose up: %w", err)
	}
	return nil
}

// applyBaselineIfFresh checks whether the database has never been
// migrated (goose_db_version table missing) and, if so, applies the
// consolidated baseline_v1.sql in a single transaction. The baseline
// recreates the schema as it stood at version 39 and INSERTs every
// historical version row into goose_db_version so the subsequent
// goose.UpContext call sees a no-op for migrations 1..39 and runs
// only the (currently zero) post-baseline migrations.
//
// Pre-existing databases are left untouched; the goose_db_version
// presence check is the cheap idempotency gate.
//
// Wave 6.2 — see docs/superpowers/plans/2026-05-09-baseline-migration.md.
func applyBaselineIfFresh(ctx context.Context, db *sql.DB) error {
	hasGoose, err := tableExists(ctx, db, "goose_db_version")
	if err != nil {
		return err
	}
	if hasGoose {
		// Existing install — leave the linear migration tail to goose.
		return nil
	}
	sqlBytes, err := sqlitebaseline.FS.ReadFile("baseline_v1.sql")
	if err != nil {
		// Missing baseline (e.g. a fork that stripped it) → fall back
		// to the linear apply path so goose.Up still produces a
		// correct schema. Log loudly so the operator sees the
		// degraded path; embed.FS surfaces missing files via the
		// fs.ErrNotExist sentinel.
		if errors.Is(err, fs.ErrNotExist) {
			slog.Default().Warn("sqlite baseline missing, falling back to linear migrations")
			return nil
		}
		return fmt.Errorf("read baseline: %w", err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin baseline tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }() //nolint:errcheck // best-effort on commit-success
	// PRAGMA foreign_keys is connection-scoped and silently ignored
	// inside a transaction in SQLite, but enabling it on the
	// tx-backing connection keeps the baseline apply symmetric with
	// the per-migration apply path (each goose migration runs under
	// foreign_keys=ON via the pool-wide DSN pragmas).
	if _, err := tx.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		return fmt.Errorf("enable FK on baseline tx: %w", err)
	}
	if _, err := tx.ExecContext(ctx, string(sqlBytes)); err != nil {
		return fmt.Errorf("exec baseline: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit baseline: %w", err)
	}
	return nil
}


func tableExists(ctx context.Context, db *sql.DB, name string) (bool, error) {
	var n int
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?",
		name,
	).Scan(&n); err != nil {
		return false, fmt.Errorf("probe %s: %w", name, err)
	}
	return n > 0, nil
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
