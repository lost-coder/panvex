package postgres

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestMigrateGoosePostgres is the PG twin of the sqlite migrate_test. It
// requires a live PostgreSQL instance reachable via PANVEX_POSTGRES_TEST_DSN.
// CI sets the env var; local dev gets an automatic skip so contributors
// without docker-pg can still run the full SQLite suite.
func TestMigrateGoosePostgres(t *testing.T) {
	dsn := os.Getenv("PANVEX_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("PANVEX_POSTGRES_TEST_DSN is not set")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	// Reset the schema so a previous test run doesn't prejudge our
	// assertions. This is destructive: never point PANVEX_POSTGRES_TEST_DSN
	// at a database you care about.
	if _, err := db.Exec(`DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		t.Fatalf("reset public schema: %v", err)
	}

	t.Run("creates_goose_version_table", func(t *testing.T) {
		if err := Migrate(db); err != nil {
			t.Fatalf("Migrate() error = %v", err)
		}
		var exists bool
		err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema='public' AND table_name='goose_db_version')`).Scan(&exists)
		if err != nil || !exists {
			t.Fatalf("goose_db_version table missing: err=%v exists=%v", err, exists)
		}
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM goose_db_version WHERE is_applied = TRUE AND version_id > 0`).Scan(&count); err != nil {
			t.Fatalf("count applied versions: %v", err)
		}
		if count < 7 {
			t.Fatalf("expected >= 7 applied goose versions, got %d", count)
		}
	})

	t.Run("idempotent", func(t *testing.T) {
		var firstCount int
		if err := db.QueryRow(`SELECT COUNT(*) FROM goose_db_version`).Scan(&firstCount); err != nil {
			t.Fatalf("count after first Migrate: %v", err)
		}
		if err := Migrate(db); err != nil {
			t.Fatalf("second Migrate() error = %v", err)
		}
		var secondCount int
		if err := db.QueryRow(`SELECT COUNT(*) FROM goose_db_version`).Scan(&secondCount); err != nil {
			t.Fatalf("count after second Migrate: %v", err)
		}
		if firstCount != secondCount {
			t.Fatalf("Migrate should be idempotent: first=%d second=%d", firstCount, secondCount)
		}
	})

	t.Run("core_tables_exist", func(t *testing.T) {
		required := []string{
			"users", "agents", "jobs", "sessions", "agent_revocations",
			"discovered_clients", "update_config", "ts_server_load",
		}
		for _, name := range required {
			var exists bool
			err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema='public' AND table_name=$1)`, name).Scan(&exists)
			if err != nil || !exists {
				t.Fatalf("expected table %q to exist: err=%v exists=%v", name, err, exists)
			}
		}
	})

	t.Run("status_smoke", func(t *testing.T) {
		if err := Status(context.Background(), db); err != nil {
			t.Fatalf("Status() error = %v", err)
		}
	})
}
