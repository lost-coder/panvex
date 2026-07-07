package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	// register the pure-Go SQLite driver under "sqlite" for database/sql
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

	// Exactly one embedded migration after the P9 squash: 0001_init.sql.
	// Pre-squash DBs carry versions 1..58, but this test always starts
	// from an empty file, so the ledger must contain exactly version 1.
	var count int
	if err := db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM goose_db_version WHERE is_applied = 1 AND version_id > 0`).Scan(&count); err != nil {
		t.Fatalf("count applied versions: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 applied goose version (0001_init), got %d", count)
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
