package migrateguard

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"path/filepath"
	"testing"

	// register the pure-Go SQLite driver under "sqlite" for database/sql
	_ "modernc.org/sqlite"
)

func openSQLite(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "guard.db"))
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func mustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

// seedGooseTable creates the goose ledger and marks the given versions
// applied — the minimum a guard probe needs to see a "not fresh" DB.
func seedGooseTable(t *testing.T, db *sql.DB, versions ...int64) {
	t.Helper()
	mustExec(t, db, `CREATE TABLE goose_db_version (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		version_id INTEGER NOT NULL,
		is_applied INTEGER NOT NULL,
		tstamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`)
	for _, v := range versions {
		mustExec(t, db, `INSERT INTO goose_db_version (version_id, is_applied) VALUES (?, 1)`, v)
	}
}

// syntheticEntry is a registry entry that no real migration ships —
// the registry itself is empty since the P9 squash, so every behaviour
// of checkOne is exercised through this synthetic value.
var syntheticEntry = DestructiveMigration{
	Version: 9999,
	Tables:  []string{"fleet_groups"},
}

// TestCheckAllEmptyRegistryIsNoOp pins the post-squash contract: with an
// empty DestructiveMigrations registry, CheckAll passes on a populated
// DB without needing PANVEX_ALLOW_DESTRUCTIVE_MIGRATION. A stale entry
// referencing a squashed-away version would permanently block migrations
// on any fresh-install DB that has data — this test is the regression
// canary for that trap.
func TestCheckAllEmptyRegistryIsNoOp(t *testing.T) {
	if len(DestructiveMigrations) != 0 {
		t.Fatalf("DestructiveMigrations must be empty after the P9 squash; got %d entries", len(DestructiveMigrations))
	}
	db := openSQLite(t)
	seedGooseTable(t, db, 1)
	mustExec(t, db, `CREATE TABLE fleet_groups (id TEXT PRIMARY KEY)`)
	mustExec(t, db, `INSERT INTO fleet_groups (id) VALUES ('fg-1')`)

	if err := CheckAll(context.Background(), db, DialectSQLite, slog.Default()); err != nil {
		t.Fatalf("CheckAll with empty registry must pass, got: %v", err)
	}
}

func TestCheckOneBlocksPopulatedUnapplied(t *testing.T) {
	t.Setenv(EnvAllowDestructiveMigration, "")
	db := openSQLite(t)
	seedGooseTable(t, db, 1)
	mustExec(t, db, `CREATE TABLE fleet_groups (id TEXT PRIMARY KEY)`)
	mustExec(t, db, `INSERT INTO fleet_groups (id) VALUES ('fg-1')`)

	err := checkOne(context.Background(), db, DialectSQLite, syntheticEntry, slog.Default())
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("expected ErrBlocked, got: %v", err)
	}
}

func TestCheckOneFreshDBPasses(t *testing.T) {
	t.Setenv(EnvAllowDestructiveMigration, "")
	db := openSQLite(t) // no goose_db_version table at all

	if err := checkOne(context.Background(), db, DialectSQLite, syntheticEntry, slog.Default()); err != nil {
		t.Fatalf("fresh DB must pass, got: %v", err)
	}
}

func TestCheckOneAppliedVersionPasses(t *testing.T) {
	t.Setenv(EnvAllowDestructiveMigration, "")
	db := openSQLite(t)
	seedGooseTable(t, db, syntheticEntry.Version)
	mustExec(t, db, `CREATE TABLE fleet_groups (id TEXT PRIMARY KEY)`)
	mustExec(t, db, `INSERT INTO fleet_groups (id) VALUES ('fg-1')`)

	if err := checkOne(context.Background(), db, DialectSQLite, syntheticEntry, slog.Default()); err != nil {
		t.Fatalf("already-applied version must pass, got: %v", err)
	}
}

func TestCheckOneEmptyTablesPass(t *testing.T) {
	t.Setenv(EnvAllowDestructiveMigration, "")
	db := openSQLite(t)
	seedGooseTable(t, db, 1)
	mustExec(t, db, `CREATE TABLE fleet_groups (id TEXT PRIMARY KEY)`)

	if err := checkOne(context.Background(), db, DialectSQLite, syntheticEntry, slog.Default()); err != nil {
		t.Fatalf("empty destructive-target tables must pass, got: %v", err)
	}
}

func TestCheckOneOptInAllows(t *testing.T) {
	t.Setenv(EnvAllowDestructiveMigration, "1")
	db := openSQLite(t)
	seedGooseTable(t, db, 1)
	mustExec(t, db, `CREATE TABLE fleet_groups (id TEXT PRIMARY KEY)`)
	mustExec(t, db, `INSERT INTO fleet_groups (id) VALUES ('fg-1')`)

	if err := checkOne(context.Background(), db, DialectSQLite, syntheticEntry, slog.Default()); err != nil {
		t.Fatalf("opt-in env must allow, got: %v", err)
	}
}
