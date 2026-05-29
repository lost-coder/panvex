package migrate

import (
	"context"
	"path/filepath"
	"testing"
)

// TestEverySchemaTableIsClassified is the guard for finding L-5: every
// table in the live SQLite schema must be classified exactly once as
// either migrated (offline copy covers it) or skipped (transient /
// recoverable, with a stated reason). A new table added to the schema
// without a classification fails this test, forcing the migrate author
// to decide whether it carries durable state.
func TestEverySchemaTableIsClassified(t *testing.T) {
	tables := listSchemaTables(t)

	// 1. migrated and skipped must not overlap.
	for name := range migratedTables {
		if _, dup := skippedTables[name]; dup {
			t.Errorf("table %q appears in BOTH migratedTables and skippedTables", name)
		}
	}

	// 2. every schema table must be classified.
	for _, name := range tables {
		_, migrated := migratedTables[name]
		_, skipped := skippedTables[name]
		if !migrated && !skipped {
			t.Errorf("schema table %q is not classified — add it to migratedTables or skippedTables (with a reason)", name)
		}
	}

	// 3. no classification entry may reference a non-existent table.
	present := make(map[string]bool, len(tables))
	for _, name := range tables {
		present[name] = true
	}
	for name := range migratedTables {
		if !present[name] {
			t.Errorf("migratedTables references %q which is not in the live schema", name)
		}
	}
	for name := range skippedTables {
		if !present[name] {
			t.Errorf("skippedTables references %q which is not in the live schema", name)
		}
	}

	// 4. every skip entry must carry a non-empty reason.
	for name, reason := range skippedTables {
		if reason == "" {
			t.Errorf("skippedTables[%q] has an empty reason", name)
		}
	}
}

// listSchemaTables opens a fresh migrated SQLite store and reads every
// user table from sqlite_master.
func listSchemaTables(t *testing.T) []string {
	t.Helper()

	store := openSQLiteStore(t, filepath.Join(t.TempDir(), "schema-list.db"))
	defer store.Close()

	db := store.(rawDBStore).DB()
	rows, err := db.QueryContext(context.Background(), `
		SELECT name FROM sqlite_master
		WHERE type = 'table' AND name NOT LIKE 'sqlite_%'
		ORDER BY name
	`)
	if err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan table name: %v", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err: %v", err)
	}
	return names
}
