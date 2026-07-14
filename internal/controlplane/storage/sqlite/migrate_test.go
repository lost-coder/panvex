package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	// register the pure-Go SQLite driver under "sqlite" for database/sql
	sqlitemigrations "github.com/lost-coder/panvex/db/migrations/sqlite"
	_ "modernc.org/sqlite"
)

// TestMigrateCreatesGooseVersionTable checks that Migrate creates the
// goose_db_version ledger and records every embedded migration. Without this
// guarantee the DF-20 finding (no schema-migration tracking) would re-open:
// an operator could not tell which DDL has been applied.
func TestMigrateCreatesGooseVersionTable(t *testing.T) {
	db := openEmptySQLite(t)

	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	// Verify the goose ledger table now exists.
	var name string
	err := db.QueryRowContext(t.Context(), `SELECT name FROM sqlite_master WHERE type='table' AND name='goose_db_version'`).Scan(&name)
	if err != nil {
		t.Fatalf("goose_db_version table not found: %v", err)
	}

	// A fresh DB applies every embedded migration: the squashed 0001_init plus
	// whatever has been added since (P9 rules: new migrations are numbered
	// >= 0059). Counting the files rather than hardcoding a number keeps this
	// from breaking every time a migration lands.
	want := countEmbeddedMigrations(t)
	var count int
	if err := db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM goose_db_version WHERE is_applied = 1 AND version_id > 0`).Scan(&count); err != nil {
		t.Fatalf("count applied versions: %v", err)
	}
	if count != want {
		t.Fatalf("applied goose versions = %d, want %d (one per embedded migration)", count, want)
	}
}

// TestMigrateCreatesMissingFKIndexes verifies that the squashed 0001_init
// carries the four FK/status indexes required by remediation task P2-DB-02
// (historically added by migration 0008; audit finding DF-22).
func TestMigrateCreatesMissingFKIndexes(t *testing.T) {
	db := openEmptySQLite(t)

	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	required := []string{
		"idx_jobs_status",
		"idx_job_targets_agent_id",
		"idx_metric_snapshots_captured_at",
		"idx_enrollment_tokens_fleet_group_id",
	}
	for _, name := range required {
		var found string
		err := db.QueryRowContext(t.Context(), `SELECT name FROM sqlite_master WHERE type='index' AND name=?`, name).Scan(&found)
		if err != nil {
			t.Fatalf("expected index %q after Migrate: %v", name, err)
		}
	}
}

// TestMigrateIsIdempotent verifies that running Migrate twice on the same
// database is a no-op at the schema level — goose must detect that every
// version is already recorded in goose_db_version and skip re-running the SQL.
func TestMigrateIsIdempotent(t *testing.T) {
	db := openEmptySQLite(t)

	if err := Migrate(db); err != nil {
		t.Fatalf("first Migrate() error = %v", err)
	}
	var firstCount int
	if err := db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM goose_db_version`).Scan(&firstCount); err != nil {
		t.Fatalf("count versions after first Migrate: %v", err)
	}

	if err := Migrate(db); err != nil {
		t.Fatalf("second Migrate() error = %v", err)
	}
	var secondCount int
	if err := db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM goose_db_version`).Scan(&secondCount); err != nil {
		t.Fatalf("count versions after second Migrate: %v", err)
	}

	if firstCount != secondCount {
		t.Fatalf("Migrate should be idempotent: first=%d second=%d", firstCount, secondCount)
	}
}

// TestMigrateCreatesCoreTables is a smoke check: after Migrate(), the tables
// every runtime path depends on must exist. This catches the case where a
// migration file silently fails to declare a needed table.
func TestMigrateCreatesCoreTables(t *testing.T) {
	db := openEmptySQLite(t)

	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	required := []string{
		"users", "user_appearance", "fleet_groups", "agents",
		"telemt_instances", "jobs", "job_targets", "audit_events",
		"metric_snapshots", "enrollment_tokens", "clients",
		"client_assignments", "client_deployments", "panel_settings",
		"certificate_authority", "agent_certificate_recovery_grants",
		"discovered_clients", "sessions", "agent_revocations",
		"update_config", "ts_server_load", "ts_dc_health",
		"ts_server_load_hourly", "client_ip_history",
	}
	for _, name := range required {
		var found string
		err := db.QueryRowContext(t.Context(), `SELECT name FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&found)
		if err != nil {
			t.Fatalf("expected table %q after Migrate: %v", name, err)
		}
	}
}

// TestSchemaUsesCanonicalJSONColumnNames pins the canonical (PostgreSQL-
// aligned) JSON column names in the SQLite schema: audit_events.details and
// metric_snapshots."values" (P2-DB-05 / DF-25, historically migration 0011).
// If a schema edit reintroduces the *_json names, the Store SQL shared
// between backends silently diverges again — this is the parity canary.
func TestSchemaUsesCanonicalJSONColumnNames(t *testing.T) {
	db := openEmptySQLite(t)

	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	cases := []struct {
		table           string
		wantContains    string
		wantNotContains string
	}{
		{"audit_events", "details ", "details_json"},
		// `values` is reserved in SQLite so the schema shows it quoted.
		{"metric_snapshots", `"values"`, "values_json"},
	}
	for _, tc := range cases {
		var createSQL string
		err := db.QueryRowContext(t.Context(),
			`SELECT sql FROM sqlite_master WHERE type='table' AND name=?`,
			tc.table,
		).Scan(&createSQL)
		if err != nil {
			t.Fatalf("read schema for %s: %v", tc.table, err)
		}
		if !strings.Contains(createSQL, tc.wantContains) {
			t.Errorf("%s schema missing expected column (%q); got: %s",
				tc.table, tc.wantContains, createSQL)
		}
		if strings.Contains(createSQL, tc.wantNotContains) {
			t.Errorf("%s schema still carries legacy column (%q); got: %s",
				tc.table, tc.wantNotContains, createSQL)
		}
	}
}

// TestMigrateSkipsSquashedBaselineOnPreSquashDB proves the upgrade path for
// databases that predate the P9 squash. Such a DB already carries goose
// versions 1..58 (the historical tree's first migration was numbered 0001), so
// when goose sees the single squashed 0001_init it MUST recognise version 1 as
// already applied and skip re-running it. If goose instead re-ran the baseline
// it would clobber tables the operator already has.
//
// The fixture is a real pre-squash DB: the product schema is materialised (by
// executing 0001_init's statements directly, WITHOUT recording them in the
// ledger, which is what a pre-squash DB looks like from goose's point of view)
// and the ledger is seeded with 1..58. A wrongful re-run of 0001_init is then
// directly observable — its CREATE TABLEs would collide with the existing ones
// and Migrate would fail.
func TestMigrateSkipsSquashedBaselineOnPreSquashDB(t *testing.T) {
	db := openEmptySQLite(t)
	ctx := t.Context()

	applySquashedSchemaWithoutLedger(t, db)

	// Recreate goose's own ledger schema, then seed it as a pre-squash DB:
	// version 0 (goose's initial row) plus 1..58 all applied.
	if _, err := db.ExecContext(ctx, `CREATE TABLE goose_db_version (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		version_id INTEGER NOT NULL,
		is_applied INTEGER NOT NULL,
		tstamp TIMESTAMP DEFAULT (datetime('now'))
	)`); err != nil {
		t.Fatalf("create goose_db_version: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO goose_db_version (version_id, is_applied) VALUES (0, 1)`); err != nil {
		t.Fatalf("seed version 0: %v", err)
	}
	for v := 1; v <= 58; v++ {
		if _, err := db.ExecContext(ctx, `INSERT INTO goose_db_version (version_id, is_applied) VALUES (?, 1)`, v); err != nil {
			t.Fatalf("seed version %d: %v", v, err)
		}
	}

	// 0001_init must be skipped (a re-run would collide with the tables that
	// already exist), while every post-squash migration must still apply.
	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate() on pre-squash DB error = %v", err)
	}

	// The ledger keeps its 58 seeded versions and gains one row per
	// post-squash migration — no duplicate row for version 1.
	postSquash := countEmbeddedMigrations(t) - 1
	var applied int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM goose_db_version WHERE is_applied = 1 AND version_id > 0`).Scan(&applied); err != nil {
		t.Fatalf("count applied versions: %v", err)
	}
	if want := 58 + postSquash; applied != want {
		t.Fatalf("applied versions = %d, want %d (58 pre-squash + %d post-squash)", applied, want, postSquash)
	}
	var version1Rows int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM goose_db_version WHERE version_id = 1`).Scan(&version1Rows); err != nil {
		t.Fatalf("count version-1 rows: %v", err)
	}
	if version1Rows != 1 {
		t.Fatalf("version 1 has %d ledger rows, want 1 (the squashed baseline must not be re-applied)", version1Rows)
	}
}

// applySquashedSchemaWithoutLedger materialises 0001_init's schema directly, so
// the DB looks like one migrated by the historical tree: the tables are there,
// but goose has no record of having created them under version 1.
func applySquashedSchemaWithoutLedger(t *testing.T, db *sql.DB) {
	t.Helper()
	raw, err := sqlitemigrations.FS.ReadFile("0001_init.sql")
	if err != nil {
		t.Fatalf("read 0001_init.sql: %v", err)
	}
	body := string(raw)
	if idx := strings.Index(body, "-- +goose Down"); idx >= 0 {
		body = body[:idx]
	}
	for _, stmt := range strings.Split(body, ";") {
		trimmed := strings.TrimSpace(stmt)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}
		if _, err := db.ExecContext(t.Context(), trimmed); err != nil {
			t.Fatalf("apply baseline statement: %v\n%s", err, trimmed)
		}
	}
}

// countEmbeddedMigrations returns how many .sql migrations ship in the binary.
func countEmbeddedMigrations(t *testing.T) int {
	t.Helper()
	entries, err := sqlitemigrations.FS.ReadDir(".")
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}
	count := 0
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			count++
		}
	}
	return count
}

// TestStatusSucceedsOnFreshDB confirms Status runs without error against a
// migrated DB. It is a smoke check; goose writes its output to stdout.
func TestStatusSucceedsOnFreshDB(t *testing.T) {
	db := openEmptySQLite(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	if err := Status(context.Background(), db); err != nil {
		t.Fatalf("Status() error = %v", err)
	}
}

func openEmptySQLite(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "migrate-test.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(t.Context(), "PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		t.Fatalf("PRAGMA foreign_keys: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
