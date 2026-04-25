package migrate

import (
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	pgmigrations "github.com/lost-coder/panvex/db/migrations/postgres"
)

// TestPostgresIndexesUseConcurrently is a guard against a class of
// migration that takes an ACCESS EXCLUSIVE lock on a populated table:
// adding a btree index without CONCURRENTLY blocks every reader and
// writer on the target table for the whole build, which on the
// audit_events / metric_snapshots / client_ip_history tables can be
// minutes to hours during a rolling upgrade.
//
// Rule: every "CREATE INDEX" in db/migrations/postgres MUST be either
//   - CONCURRENTLY, or
//   - against a table created in the same migration file (no live
//     traffic to lock), in which case the file simply contains a
//     matching CREATE TABLE.
//
// CONCURRENTLY is illegal inside a transaction, so a file using it
// must also start with `-- +goose NO TRANSACTION`.
func TestPostgresIndexesUseConcurrently(t *testing.T) {
	// Identifier = unquoted SQL ident or "double-quoted ident"; either may
	// be schema-qualified. Stops at whitespace, `(`, or `;` so the
	// column-list form `table(col)` is not captured into the table name.
	identifier := `(?:"[^"]+"|[A-Za-z_][A-Za-z0-9_]*)(?:\.(?:"[^"]+"|[A-Za-z_][A-Za-z0-9_]*))?`
	createIndexRE := regexp.MustCompile(`(?im)^\s*CREATE\s+(UNIQUE\s+)?INDEX(\s+CONCURRENTLY)?(\s+IF\s+NOT\s+EXISTS)?\s+` + identifier + `\s+ON\s+(` + identifier + `)`)
	createTableRE := regexp.MustCompile(`(?im)^\s*CREATE\s+TABLE(\s+IF\s+NOT\s+EXISTS)?\s+(` + identifier + `)`)

	err := fs.WalkDir(pgmigrations.FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".sql") {
			return nil
		}

		raw, err := fs.ReadFile(pgmigrations.FS, path)
		if err != nil {
			return err
		}
		body := string(raw)

		tablesCreatedHere := map[string]bool{}
		for _, m := range createTableRE.FindAllStringSubmatch(body, -1) {
			tablesCreatedHere[normaliseTable(m[2])] = true
		}

		usesConcurrently := false
		for _, m := range createIndexRE.FindAllStringSubmatch(body, -1) {
			concurrently := strings.TrimSpace(m[2]) != ""
			target := normaliseTable(m[4])
			switch {
			case concurrently:
				usesConcurrently = true
			case tablesCreatedHere[target]:
				// Index on a table created in the same migration: lock
				// is uncontested because the table is empty and not yet
				// referenced by any read/write traffic.
			default:
				t.Errorf("%s: CREATE INDEX on existing table %q must use CONCURRENTLY (%s)", filepath.Base(path), target, m[0])
			}
		}

		if usesConcurrently && !strings.Contains(body, "+goose NO TRANSACTION") {
			t.Errorf("%s: uses CREATE INDEX CONCURRENTLY but lacks `-- +goose NO TRANSACTION` pragma", filepath.Base(path))
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk migrations: %v", err)
	}
}

func normaliseTable(raw string) string {
	s := strings.TrimSuffix(raw, "(")
	s = strings.TrimSpace(s)
	s = strings.Trim(s, `"`)
	return strings.ToLower(s)
}
