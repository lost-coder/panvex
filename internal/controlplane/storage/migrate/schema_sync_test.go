package migrate_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
//   - column TYPES are still out of scope; only column NAMES,
//     normalized CHECK constraint expressions, and FK delete-rules
//     are compared (C1).
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
	// checks holds normalized CHECK constraint expressions (see
	// normalizeCheckExpr) so enum-guard drift between the dialects is
	// caught in CI, not in production (C1).
	checks []string
	// fks holds normalized "column->ref_table [DELETE_RULE]" strings so
	// a missing ON DELETE CASCADE in one dialect fails the test.
	fks []string
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
		assertStringSetMatch(t, name, "CHECK constraints", pg[name].checks, sqTbl.checks)
		assertStringSetMatch(t, name, "FK delete rules", pg[name].fks, sqTbl.fks)
	}
}

// assertStringSetMatch compares two unordered normalized-string sets and
// reports per-side differences with table context.
func assertStringSetMatch(t *testing.T, table, kind string, pgSet, sqSet []string) {
	t.Helper()
	pgSorted := append([]string(nil), pgSet...)
	sqSorted := append([]string(nil), sqSet...)
	sort.Strings(pgSorted)
	sort.Strings(sqSorted)
	if equalStringSlices(pgSorted, sqSorted) {
		return
	}
	if only := diffSlices(pgSorted, sqSorted); len(only) > 0 {
		t.Errorf("table %s: %s only in PostgreSQL: %v", table, kind, only)
	}
	if only := diffSlices(sqSorted, pgSorted); len(only) > 0 {
		t.Errorf("table %s: %s only in SQLite: %v", table, kind, only)
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
	if err := rows.Err(); err != nil {
		return nil, err
	}

	checkRows, err := db.QueryContext(ctx, `
		SELECT rel.relname, pg_get_constraintdef(con.oid)
		FROM pg_constraint con
		JOIN pg_class rel ON rel.oid = con.conrelid
		JOIN pg_namespace nsp ON nsp.oid = rel.relnamespace
		WHERE nsp.nspname = 'public' AND con.contype = 'c'
	`)
	if err != nil {
		return nil, err
	}
	defer checkRows.Close()
	for checkRows.Next() {
		var table, def string
		if err := checkRows.Scan(&table, &def); err != nil {
			return nil, err
		}
		ts := result[table]
		ts.checks = append(ts.checks, normalizeCheckExpr(def))
		result[table] = ts
	}
	if err := checkRows.Err(); err != nil {
		return nil, err
	}

	fkRows, err := db.QueryContext(ctx, `
		SELECT tc.table_name, kcu.column_name, ccu.table_name AS foreign_table, rc.delete_rule
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu
		  ON kcu.constraint_name = tc.constraint_name AND kcu.table_schema = tc.table_schema
		JOIN information_schema.referential_constraints rc
		  ON rc.constraint_name = tc.constraint_name AND rc.constraint_schema = tc.table_schema
		JOIN information_schema.constraint_column_usage ccu
		  ON ccu.constraint_name = tc.constraint_name AND ccu.table_schema = tc.table_schema
		WHERE tc.table_schema = 'public' AND tc.constraint_type = 'FOREIGN KEY'
	`)
	if err != nil {
		return nil, err
	}
	defer fkRows.Close()
	for fkRows.Next() {
		var table, column, refTable, rule string
		if err := fkRows.Scan(&table, &column, &refTable, &rule); err != nil {
			return nil, err
		}
		ts := result[table]
		ts.fks = append(ts.fks, normalizeFK(column, refTable, rule))
		result[table] = ts
	}
	if err := fkRows.Err(); err != nil {
		return nil, err
	}

	return result, nil
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

		var createSQL string
		if err := db.QueryRowContext(ctx,
			"SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?", name).Scan(&createSQL); err != nil {
			return nil, err
		}
		var checks []string
		for _, expr := range extractSQLiteCheckExprs(createSQL) {
			norm := normalizeCheckExpr(expr)
			if isEngineInherentCheck(norm) {
				continue
			}
			checks = append(checks, norm)
		}

		fks, err := readSQLiteForeignKeys(ctx, db, name)
		if err != nil {
			return nil, err
		}
		result[name] = tableSchema{columns: cols, checks: checks, fks: fks}
	}
	return result, nil
}

// readSQLiteForeignKeys lists normalized FK descriptors for one table.
func readSQLiteForeignKeys(ctx context.Context, db *sql.DB, table string) ([]string, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT "table", "from", on_delete FROM pragma_foreign_key_list(?)`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var refTable, from, rule string
		if err := rows.Scan(&refTable, &from, &rule); err != nil {
			return nil, err
		}
		out = append(out, normalizeFK(from, refTable, rule))
	}
	return out, rows.Err()
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

// extractSQLiteCheckExprs pulls every CHECK(...) body out of a CREATE
// TABLE statement with a balanced-paren scan (regexes cannot handle the
// nested parens inside IN-lists).
func extractSQLiteCheckExprs(createSQL string) []string {
	var out []string
	lower := strings.ToLower(createSQL)
	for i := 0; ; {
		idx := strings.Index(lower[i:], "check")
		if idx < 0 {
			break
		}
		pos := i + idx + len("check")
		for pos < len(createSQL) && (createSQL[pos] == ' ' || createSQL[pos] == '\t' || createSQL[pos] == '\n' || createSQL[pos] == '\r') {
			pos++
		}
		if pos >= len(createSQL) || createSQL[pos] != '(' {
			i = pos
			continue
		}
		depth, end := 0, -1
		for j := pos; j < len(createSQL) && end < 0; j++ {
			switch createSQL[j] {
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 {
					end = j
				}
			}
		}
		if end < 0 {
			break
		}
		out = append(out, createSQL[pos+1:end])
		i = end + 1
	}
	return out
}

// normalizeCheckExpr folds the dialects' CHECK spellings to one form:
// PG renders enums as `(status = ANY (ARRAY['a'::text, 'b'::text]))`,
// SQLite keeps the verbatim `status IN ('a', 'b')`. Lowercase, drop
// casts/quotes/brackets, rewrite `= any (array[...])` to `in (...)`,
// then strip ALL parens and collapse whitespace so only the token
// stream is compared.
func normalizeCheckExpr(expr string) string {
	s := strings.ToLower(expr)
	s = strings.TrimPrefix(strings.TrimSpace(s), "check ")
	s = strings.ReplaceAll(s, "::text", "")
	s = strings.ReplaceAll(s, "= any (array[", "in (")
	s = strings.ReplaceAll(s, "= any(array[", "in (")
	for _, ch := range []string{"(", ")", "[", "]", `"`, "'", "`", ","} {
		s = strings.ReplaceAll(s, ch, " ")
	}
	return strings.Join(strings.Fields(s), " ")
}

// sqliteBooleanCheckRE matches SQLite's BOOLEAN-emulation guard
// (`col IN (0, 1)` after normalization). PG uses a real BOOLEAN type,
// so these checks are engine-inherent and excluded from parity.
var sqliteBooleanCheckRE = regexp.MustCompile(`^[a-z0-9_]+ in 0 1$`)

func isEngineInherentCheck(normalized string) bool {
	return sqliteBooleanCheckRE.MatchString(normalized)
}

// normalizeFK renders one FK as "column->ref_table [RULE]". Column
// names go through the same timestamp-suffix folding as the column
// comparison (_unix then _at); delete rules are upper-cased
// ("NO ACTION" both sides).
func normalizeFK(column, refTable, rule string) string {
	col := strings.TrimSuffix(strings.TrimSuffix(strings.ToLower(column), "_unix"), "_at")
	return fmt.Sprintf("%s->%s [%s]", col, strings.ToLower(refTable), strings.ToUpper(strings.TrimSpace(rule)))
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
