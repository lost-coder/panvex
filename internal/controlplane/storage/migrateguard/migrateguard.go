// Package migrateguard refuses to apply migrations that would silently
// destroy production data unless the operator has explicitly opted in.
//
// Some early-life migrations (notably 0014_fleet_groups_redesign) ship
// with `DELETE FROM` against tables that hold real production rows.
// On a fresh DB those statements are no-ops; on a populated DB they
// erase fleet, agent and client state that cannot be reconstructed
// without restore-from-backup. The check below sits between the goose
// runner and `UpContext` so the unsafe path can never be taken without
// PANVEX_ALLOW_DESTRUCTIVE_MIGRATION=1 in the environment.
package migrateguard

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
)

// EnvAllowDestructiveMigration is the env var operators set when they
// have a current backup and accept that the migration will erase rows
// from the listed tables. Any non-empty value counts as opt-in.
const EnvAllowDestructiveMigration = "PANVEX_ALLOW_DESTRUCTIVE_MIGRATION"

// ErrBlocked is returned by CheckAll when a destructive migration is
// pending against a non-empty DB and no opt-in is set.
var ErrBlocked = errors.New("destructive migration blocked: set PANVEX_ALLOW_DESTRUCTIVE_MIGRATION=1 after taking a backup")

// Dialect selects the SQL flavour used by the existence/count probes.
type Dialect int

const (
	DialectPostgres Dialect = iota
	DialectSQLite
)

// DestructiveMigration is one entry in the registry below: the goose
// version the migration ships under and the tables whose rows it
// destroys. New entries are appended whenever a future migration adds
// another DROP/TRUNCATE/DELETE step.
//
// Per-dialect differences are handled at the CALL SITE — a benign
// dialect (e.g. SQLite 0014, which uses ADD COLUMN + UPDATE) simply
// does not call CheckAll. Keep the call-side wiring in
// internal/controlplane/storage/{postgres,sqlite}/migrate.go in sync
// with this list when adding a new entry.
type DestructiveMigration struct {
	Version int64
	Tables  []string
}

// DestructiveMigrations is the canonical registry of destructive
// migrations.
var DestructiveMigrations = []DestructiveMigration{
	{
		Version: 14,
		Tables:  []string{"fleet_groups", "agents", "clients"},
	},
}

// CheckAll runs the guard for every entry in DestructiveMigrations and
// returns the first ErrBlocked it produces (or nil if none apply).
func CheckAll(ctx context.Context, db *sql.DB, dialect Dialect, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}
	for _, m := range DestructiveMigrations {
		if err := checkOne(ctx, db, dialect, m, logger); err != nil {
			return err
		}
	}
	return nil
}

func checkOne(ctx context.Context, db *sql.DB, dialect Dialect, m DestructiveMigration, logger *slog.Logger) error {
	exists, err := tableExists(ctx, db, dialect, "goose_db_version")
	if err != nil {
		return fmt.Errorf("destructive-guard: probe goose_db_version: %w", err)
	}
	if !exists {
		// Fresh DB — nothing to lose.
		return nil
	}

	applied, err := versionApplied(ctx, db, dialect, m.Version)
	if err != nil {
		return fmt.Errorf("destructive-guard: read goose_db_version: %w", err)
	}
	if applied {
		// Already executed in a prior run; whatever DELETE/DROP was going
		// to happen has already happened. Idempotent re-runs are safe.
		return nil
	}

	populated, err := anyTableHasRows(ctx, db, dialect, m.Tables)
	if err != nil {
		return fmt.Errorf("destructive-guard: count rows: %w", err)
	}
	if !populated {
		return nil
	}

	if strings.TrimSpace(os.Getenv(EnvAllowDestructiveMigration)) != "" {
		logger.Warn("applying destructive migration with operator opt-in",
			"version", m.Version,
			"tables", m.Tables,
			"env", EnvAllowDestructiveMigration,
		)
		return nil
	}

	return fmt.Errorf("%w: migration %d would erase rows from %v",
		ErrBlocked, m.Version, m.Tables)
}

func tableExists(ctx context.Context, db *sql.DB, dialect Dialect, name string) (bool, error) {
	var query string
	switch dialect {
	case DialectPostgres:
		query = `SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = current_schema() AND table_name = $1
		)`
	case DialectSQLite:
		query = `SELECT EXISTS (
			SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = ?
		)`
	default:
		return false, fmt.Errorf("unknown dialect %v", dialect)
	}
	var exists bool
	if err := db.QueryRowContext(ctx, query, name).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

func versionApplied(ctx context.Context, db *sql.DB, dialect Dialect, version int64) (bool, error) {
	// goose v3 schema: goose_db_version (id, version_id, is_applied, tstamp).
	// Multiple rows per version_id can exist if goose was rolled back and
	// re-applied; the row with the largest id carries the live state.
	// Returning int and comparing to 1 sidesteps driver-level differences
	// in scanning bool from a 0/1 column.
	var query string
	switch dialect {
	case DialectPostgres:
		query = `SELECT COALESCE(MAX(CASE WHEN is_applied THEN 1 ELSE 0 END), 0)
		         FROM goose_db_version WHERE version_id = $1`
	case DialectSQLite:
		query = `SELECT COALESCE(MAX(CASE WHEN is_applied THEN 1 ELSE 0 END), 0)
		         FROM goose_db_version WHERE version_id = ?`
	default:
		return false, fmt.Errorf("unknown dialect %v", dialect)
	}
	var applied int
	if err := db.QueryRowContext(ctx, query, version).Scan(&applied); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return applied == 1, nil
}

func anyTableHasRows(ctx context.Context, db *sql.DB, dialect Dialect, tables []string) (bool, error) {
	for _, t := range tables {
		exists, err := tableExists(ctx, db, dialect, t)
		if err != nil {
			return false, err
		}
		if !exists {
			continue
		}
		// LIMIT 1 keeps the probe cheap on huge tables.
		var found int
		// Table name interpolation is safe here because t comes from the
		// hard-coded DestructiveMigrations registry above, not from any
		// external input. Identifier quoting is dialect-specific but
		// fleet_groups / agents / clients are simple lower-case idents
		// that need no escaping.
		query := fmt.Sprintf("SELECT 1 FROM %s LIMIT 1", t) //nolint:gosec // G201: identifier from hard-coded registry, never user input
		err = db.QueryRowContext(ctx, query).Scan(&found)
		switch {
		case err == nil:
			return true, nil
		case errors.Is(err, sql.ErrNoRows):
			continue
		default:
			return false, fmt.Errorf("probe %s: %w", t, err)
		}
	}
	return false, nil
}
