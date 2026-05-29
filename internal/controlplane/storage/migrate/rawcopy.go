package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// rawDBStore is the narrow accessor the raw row-copy path needs. Both
// sqlite.Store and postgres.Store expose DB() *sql.DB. The interface is
// declared here (not in storage) so production code that handles a
// storage.MigrationStore never gains raw *sql.DB access — only the
// offline migrate tooling type-asserts to it.
type rawDBStore interface {
	DB() *sql.DB
}

// copyTableRaw streams every row of `table` from source to target with a
// dynamic SELECT * → INSERT. It is used for tables whose Go-typed
// accessors would re-encrypt ciphertext (webhook_endpoints) or that live
// in a separate store package outside MigrationStore (runtime_settings,
// webhook_outbox). Ciphertext and opaque blobs are copied byte-for-byte
// because the values pass through as driver `any` scan targets — no
// domain decode/encode happens.
//
// Columns are discovered dynamically from the source result set so the
// helper is resilient to column-order or additive-column differences;
// placeholders are emitted in the target dialect (sqlite "?", postgres
// "$N"). Returns the number of rows copied.
func copyTableRaw(ctx context.Context, source, target storage.MigrationStore, table string, targetUsesDollarPlaceholders bool) (int, error) {
	src, ok := source.(rawDBStore)
	if !ok {
		return 0, fmt.Errorf("migrate: source store does not expose a raw *sql.DB for table %q", table)
	}
	dst, ok := target.(rawDBStore)
	if !ok {
		return 0, fmt.Errorf("migrate: target store does not expose a raw *sql.DB for table %q", table)
	}

	srcDB := src.DB()
	dstDB := dst.DB()
	if srcDB == nil || dstDB == nil {
		return 0, fmt.Errorf("migrate: raw *sql.DB unavailable for table %q (tx-bound store)", table)
	}

	// Identifier is from the hard-coded migratedTables registry, never
	// user input.
	rows, err := srcDB.QueryContext(ctx, fmt.Sprintf("SELECT * FROM %s", table)) //nolint:gosec // G201: table from internal registry
	if err != nil {
		return 0, fmt.Errorf("migrate: select %s: %w", table, err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return 0, fmt.Errorf("migrate: columns %s: %w", table, err)
	}

	insertSQL := buildInsert(table, cols, targetUsesDollarPlaceholders)

	count := 0
	for rows.Next() {
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return 0, fmt.Errorf("migrate: scan %s: %w", table, err)
		}
		if _, err := dstDB.ExecContext(ctx, insertSQL, values...); err != nil {
			return 0, fmt.Errorf("migrate: insert %s: %w", table, err)
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("migrate: iterate %s: %w", table, err)
	}
	return count, nil
}

// countTableRaw returns the row count of `table` via a raw COUNT(*).
func countTableRaw(ctx context.Context, store storage.MigrationStore, table string) (int, error) {
	s, ok := store.(rawDBStore)
	if !ok {
		return 0, fmt.Errorf("migrate: store does not expose a raw *sql.DB for table %q", table)
	}
	db := s.DB()
	if db == nil {
		return 0, fmt.Errorf("migrate: raw *sql.DB unavailable for table %q (tx-bound store)", table)
	}
	var n int
	// Identifier from internal registry, never user input.
	if err := db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&n); err != nil { //nolint:gosec // G201: table from internal registry
		return 0, fmt.Errorf("migrate: count %s: %w", table, err)
	}
	return n, nil
}

// buildInsert renders an INSERT with one placeholder per column in the
// target dialect.
func buildInsert(table string, cols []string, dollar bool) string {
	placeholders := make([]string, len(cols))
	for i := range cols {
		if dollar {
			placeholders[i] = fmt.Sprintf("$%d", i+1)
		} else {
			placeholders[i] = "?"
		}
	}
	// Identifiers from the result-set / internal registry, never user input.
	// Column names are double-quoted so a column that collides with a SQL
	// reserved word (e.g. a "values" column) does not break the INSERT;
	// double-quoting is the ANSI identifier quote honoured by both SQLite
	// and Postgres.
	return fmt.Sprintf(`INSERT INTO %s (%s) VALUES (%s)`,
		table,
		`"`+strings.Join(cols, `", "`)+`"`,
		strings.Join(placeholders, ", "),
	)
}
