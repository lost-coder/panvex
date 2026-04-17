package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

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
	err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='goose_db_version'`).Scan(&name)
	if err != nil {
		t.Fatalf("goose_db_version table not found: %v", err)
	}

	// Count applied versions — we expect at least the 8 embedded migrations
	// (0001..0008). The ">=" floor is there so future migrations don't
	// force a brittle equality assertion.
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM goose_db_version WHERE is_applied = 1 AND version_id > 0`).Scan(&count); err != nil {
		t.Fatalf("count applied versions: %v", err)
	}
	if count < 8 {
		t.Fatalf("expected >= 8 applied goose versions, got %d", count)
	}
}

// TestMigrateCreatesMissingFKIndexes verifies that migration 0008 creates the
// four FK/status indexes required by remediation task P2-DB-02. Without these,
// audit finding DF-22 (missing indexes on high-selectivity foreign keys) stays
// open — the retention worker and filter APIs would degrade to full scans.
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
		err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='index' AND name=?`, name).Scan(&found)
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
	if err := db.QueryRow(`SELECT COUNT(*) FROM goose_db_version`).Scan(&firstCount); err != nil {
		t.Fatalf("count versions after first Migrate: %v", err)
	}

	if err := Migrate(db); err != nil {
		t.Fatalf("second Migrate() error = %v", err)
	}
	var secondCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM goose_db_version`).Scan(&secondCount); err != nil {
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
		err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&found)
		if err != nil {
			t.Fatalf("expected table %q after Migrate: %v", name, err)
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
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		t.Fatalf("PRAGMA foreign_keys: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
