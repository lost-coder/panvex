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
	dsn := "file:" + filepath.Join(t.TempDir(), "guard.db")
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// schema bootstraps just enough of the production layout for the guard
// to make a meaningful decision: the goose bookkeeping table plus the
// three tables that DestructiveMigrations[0] guards.
func schema(t *testing.T, db *sql.DB) {
	t.Helper()
	stmts := []string{
		`CREATE TABLE goose_db_version (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			version_id BIGINT NOT NULL,
			is_applied INTEGER NOT NULL,
			tstamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE fleet_groups (id TEXT PRIMARY KEY, name TEXT)`,
		`CREATE TABLE agents (id TEXT PRIMARY KEY)`,
		`CREATE TABLE clients (id TEXT PRIMARY KEY)`,
	}
	for _, s := range stmts {
		if _, err := db.ExecContext(t.Context(), s); err != nil {
			t.Fatalf("schema %q: %v", s, err)
		}
	}
}

func TestCheckAll_FreshDB_NoGooseTable(t *testing.T) {
	t.Setenv(EnvAllowDestructiveMigration, "")
	db := openSQLite(t)
	if err := CheckAll(context.Background(), db, DialectSQLite, slog.Default()); err != nil {
		t.Fatalf("fresh DB should pass, got %v", err)
	}
}

func TestCheckAll_VersionAlreadyApplied(t *testing.T) {
	t.Setenv(EnvAllowDestructiveMigration, "")
	db := openSQLite(t)
	schema(t, db)
	// Mark version 14 as applied AND populate the tables: the migration
	// already ran, so the data here is post-erase state and must not
	// trigger the guard.
	if _, err := db.ExecContext(t.Context(), `INSERT INTO goose_db_version (version_id, is_applied) VALUES (14, 1)`); err != nil {
		t.Fatalf("seed goose_db_version: %v", err)
	}
	if _, err := db.ExecContext(t.Context(), `INSERT INTO fleet_groups (id, name) VALUES ('g1', 'x')`); err != nil {
		t.Fatalf("seed fleet_groups: %v", err)
	}

	if err := CheckAll(context.Background(), db, DialectSQLite, slog.Default()); err != nil {
		t.Fatalf("already-applied should pass, got %v", err)
	}
}

func TestCheckAll_PendingButTablesEmpty(t *testing.T) {
	t.Setenv(EnvAllowDestructiveMigration, "")
	db := openSQLite(t)
	schema(t, db)
	// goose table exists but version 14 is not in it; tables empty.
	if err := CheckAll(context.Background(), db, DialectSQLite, slog.Default()); err != nil {
		t.Fatalf("empty tables should pass, got %v", err)
	}
}

func TestCheckAll_PendingAndPopulated_BlocksByDefault(t *testing.T) {
	t.Setenv(EnvAllowDestructiveMigration, "")
	db := openSQLite(t)
	schema(t, db)
	if _, err := db.ExecContext(t.Context(), `INSERT INTO fleet_groups (id, name) VALUES ('g1', 'production')`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	err := CheckAll(context.Background(), db, DialectSQLite, slog.Default())
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("want ErrBlocked, got %v", err)
	}
}

func TestCheckAll_PendingAndPopulated_AllowedByEnv(t *testing.T) {
	t.Setenv(EnvAllowDestructiveMigration, "1")
	db := openSQLite(t)
	schema(t, db)
	if _, err := db.ExecContext(t.Context(), `INSERT INTO agents (id) VALUES ('a1')`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := CheckAll(context.Background(), db, DialectSQLite, slog.Default()); err != nil {
		t.Fatalf("opt-in should pass, got %v", err)
	}
}

func TestCheckAll_TableMissing_DoesNotPanic(t *testing.T) {
	t.Setenv(EnvAllowDestructiveMigration, "")
	db := openSQLite(t)
	// goose table exists but the data tables do not — earliest schema state.
	if _, err := db.ExecContext(t.Context(), `CREATE TABLE goose_db_version (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		version_id BIGINT NOT NULL,
		is_applied INTEGER NOT NULL,
		tstamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		t.Fatal(err)
	}
	if err := CheckAll(context.Background(), db, DialectSQLite, slog.Default()); err != nil {
		t.Fatalf("missing data tables should pass, got %v", err)
	}
}

func TestCheckAll_BlankEnvDoesNotCountAsOptIn(t *testing.T) {
	// Explicitly set to whitespace; should be treated as unset.
	t.Setenv(EnvAllowDestructiveMigration, "   ")
	db := openSQLite(t)
	schema(t, db)
	if _, err := db.ExecContext(t.Context(), `INSERT INTO clients (id) VALUES ('c1')`); err != nil {
		t.Fatal(err)
	}
	if err := CheckAll(context.Background(), db, DialectSQLite, slog.Default()); !errors.Is(err, ErrBlocked) {
		t.Fatalf("want ErrBlocked despite whitespace-only env, got %v", err)
	}
}
