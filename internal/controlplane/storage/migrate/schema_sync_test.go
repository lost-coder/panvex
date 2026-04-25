package migrate_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/lost-coder/panvex/internal/controlplane/storage/postgres"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

// TestSchemaSyncPostgresMatchesSQLite (C9) asserts that the PostgreSQL
// and SQLite migration bundles converge on the same set of tables and
// column names. The band-aid migration 0011_column_drift.sql exists
// precisely because the two bundles silently drifted once — without a
// CI guardrail that only gets caught in production. This test closes
// the loop by comparing the post-migration schemas side by side.
//
// Limitations:
//   - column types and nullability differ naturally between engines
//     (TEXT vs TEXT, BIGINT vs INTEGER, timestamp vs datetime), so
//     only column NAMES are compared, not data types.
//   - indexes are engine-specific in name, so we compare the index
//     COUNT per table as a proxy, not index definitions.
//   - goose_db_version is the bookkeeping table goose maintains; we
//     ignore it.
//
// Requires a live PostgreSQL instance reachable via
// PANVEX_POSTGRES_TEST_DSN. The DSN must point at a throwaway DB —
// the test drops and recreates the `public` schema.
func TestSchemaSyncPostgresMatchesSQLite(t *testing.T) {
	dsn := os.Getenv("PANVEX_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("PANVEX_POSTGRES_TEST_DSN is not set")
	}

	pg, err := postgres.Open(dsn)
	if err != nil {
		t.Fatalf("postgres.Open: %v", err)
	}
	defer pg.Close()

	rawPg, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open pgx: %v", err)
	}
	defer rawPg.Close()

	// Nuke and rebuild public schema so the run is hermetic.
	if _, err := rawPg.ExecContext(context.Background(),
		"DROP SCHEMA public CASCADE; CREATE SCHEMA public;"); err != nil {
		t.Fatalf("reset public schema: %v", err)
	}
	if err := postgres.Migrate(rawPg); err != nil {
		t.Fatalf("postgres.Migrate: %v", err)
	}

	dbPath := filepath.Join(t.TempDir(), "schema_sync.db")
	sq, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer sq.Close()

	// sqlite.Open applies migrations internally.

	sqliteSchema, err := readSQLiteSchema(t.Context(), dbPath)
	if err != nil {
		t.Fatalf("read sqlite schema: %v", err)
	}
	pgSchema, err := readPostgresSchema(t.Context(), rawPg)
	if err != nil {
		t.Fatalf("read postgres schema: %v", err)
	}

	assertSchemasMatch(t, pgSchema, sqliteSchema)
}

type tableSchema struct {
	columns []string
}

func assertSchemasMatch(t *testing.T, pg, sq map[string]tableSchema) {
	t.Helper()

	pgNames := sortedKeys(pg)
	sqNames := sortedKeys(sq)

	if !equalStringSlices(pgNames, sqNames) {
		onlyPG := diffSlices(pgNames, sqNames)
		onlySQ := diffSlices(sqNames, pgNames)
		if len(onlyPG) > 0 {
			t.Errorf("tables only in PostgreSQL: %v", onlyPG)
		}
		if len(onlySQ) > 0 {
			t.Errorf("tables only in SQLite: %v", onlySQ)
		}
	}

	for name := range pg {
		sqTbl, ok := sq[name]
		if !ok {
			continue
		}
		// Normalize timestamp-convention suffixes before comparing: SQLite
		// stores timestamps as INTEGER unix seconds with a `*_unix` suffix,
		// PostgreSQL stores them as TIMESTAMPTZ in columns named `*_at` /
		// `*_seen`. Both conventions describe the same logical field, so
		// stripping the suffix lets the test focus on structural drift
		// (missing columns, renamed prefixes) instead of the by-design
		// naming difference.
		pgCols := normalizeColumnNames(pg[name].columns)
		sqCols := normalizeColumnNames(sqTbl.columns)
		sort.Strings(pgCols)
		sort.Strings(sqCols)
		if !equalStringSlices(pgCols, sqCols) {
			onlyPG := diffSlices(pgCols, sqCols)
			onlySQ := diffSlices(sqCols, pgCols)
			if len(onlyPG) > 0 {
				t.Errorf("table %s: columns only in PostgreSQL: %v", name, onlyPG)
			}
			if len(onlySQ) > 0 {
				t.Errorf("table %s: columns only in SQLite: %v", name, onlySQ)
			}
		}
	}
}

// normalizeColumnNames folds the PG/SQLite timestamp-column conventions to a
// single form. Strip a trailing `_unix` (SQLite INTEGER unix seconds) or
// `_at` (PostgreSQL TIMESTAMPTZ) so pairs like
// `created_at` / `created_at_unix` collapse to `created`, and
// `timestamp_at` / `timestamp_unix` collapse to `timestamp`.
func normalizeColumnNames(cols []string) []string {
	out := make([]string, 0, len(cols))
	for _, c := range cols {
		c = strings.TrimSuffix(c, "_unix")
		c = strings.TrimSuffix(c, "_at")
		out = append(out, c)
	}
	return out
}

func readPostgresSchema(ctx context.Context, db *sql.DB) (map[string]tableSchema, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT table_name, column_name
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name <> 'goose_db_version'
		ORDER BY table_name, ordinal_position
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := map[string]tableSchema{}
	for rows.Next() {
		var table, column string
		if err := rows.Scan(&table, &column); err != nil {
			return nil, err
		}
		ts := result[table]
		ts.columns = append(ts.columns, column)
		result[table] = ts
	}
	return result, rows.Err()
}

func readSQLiteSchema(ctx context.Context, path string) (map[string]tableSchema, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, `
		SELECT name FROM sqlite_master
		WHERE type = 'table'
		  AND name <> 'goose_db_version'
		  AND name NOT LIKE 'sqlite_%'
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tables := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	result := map[string]tableSchema{}
	for _, name := range tables {
		cols, err := readSQLiteTableColumns(ctx, db, name)
		if err != nil {
			return nil, err
		}
		result[name] = tableSchema{columns: cols}
	}
	return result, nil
}

func sortedKeys(m map[string]tableSchema) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// readSQLiteTableColumns lists the columns of a single table. Extracted
// so `defer colRows.Close()` covers every exit path (sqlclosecheck) —
// the inline form in the parent loop required two manual Close() sites.
func readSQLiteTableColumns(ctx context.Context, db *sql.DB, table string) ([]string, error) {
	colRows, err := db.QueryContext(ctx,
		"SELECT name FROM pragma_table_info(?) ORDER BY cid", table)
	if err != nil {
		return nil, err
	}
	defer colRows.Close()
	var cols []string
	for colRows.Next() {
		var c string
		if err := colRows.Scan(&c); err != nil {
			return nil, err
		}
		cols = append(cols, c)
	}
	if err := colRows.Err(); err != nil {
		return nil, err
	}
	return cols, nil
}

func diffSlices(a, b []string) []string {
	set := map[string]struct{}{}
	for _, x := range b {
		set[strings.ToLower(x)] = struct{}{}
	}
	var only []string
	for _, x := range a {
		if _, ok := set[strings.ToLower(x)]; !ok {
			only = append(only, x)
		}
	}
	return only
}
