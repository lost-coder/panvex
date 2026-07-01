package migrate

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"

	sqlitemigrations "github.com/lost-coder/panvex/db/migrations/sqlite"
)

// TestSQLiteTableRebuildsAreTransactionWrapped is a guard against the M5
// audit finding: SQLite migrations that rebuild a table (SQLite has no
// ALTER TABLE ADD/DROP CONSTRAINT) run the whole file under
// `-- +goose NO TRANSACTION` so that `PRAGMA foreign_keys` can be toggled
// (SQLite forbids toggling that pragma inside a transaction). But without
// its own transaction, a crash between `DROP TABLE x` and
// `ALTER TABLE x_new RENAME TO x` leaves the table gone and the migration
// un-recorded: re-running the migration then fails because `x_new` already
// exists and `x` does not.
//
// Rule: every `DROP TABLE <t>` immediately followed (module intervening
// index/comment noise) by `ALTER TABLE <t>_new RENAME TO <t>` must sit
// inside an explicit BEGIN/COMMIT pair, so the rebuild is atomic even
// though the file as a whole runs outside goose's own transaction.
func TestSQLiteTableRebuildsAreTransactionWrapped(t *testing.T) {
	dropRE := regexp.MustCompile(`(?im)^\s*DROP\s+TABLE\s+([A-Za-z_][A-Za-z0-9_]*)\s*;`)
	renameRE := regexp.MustCompile(`(?im)^\s*ALTER\s+TABLE\s+([A-Za-z_][A-Za-z0-9_]*)_new\s+RENAME\s+TO\s+([A-Za-z_][A-Za-z0-9_]*)\s*;`)
	beginRE := regexp.MustCompile(`(?im)^\s*BEGIN\s*;`)
	commitRE := regexp.MustCompile(`(?im)^\s*COMMIT\s*;`)

	err := fs.WalkDir(sqlitemigrations.FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".sql") {
			return nil
		}

		raw, err := fs.ReadFile(sqlitemigrations.FS, path)
		if err != nil {
			return err
		}
		body := string(raw)

		if !strings.Contains(body, "+goose NO TRANSACTION") {
			// Rebuild files that need PRAGMA foreign_keys toggling must run
			// outside goose's own transaction; files that don't declare this
			// are not doing the DROP/RENAME rebuild dance this guard cares
			// about (or goose already wraps them safely).
			return nil
		}

		drops := dropRE.FindAllStringSubmatchIndex(body, -1)
		if len(drops) == 0 {
			return nil
		}

		begins := beginRE.FindAllStringIndex(body, -1)
		commits := commitRE.FindAllStringIndex(body, -1)

		for _, d := range drops {
			table := body[d[2]:d[3]]
			dropPos := d[0]

			// Find the RENAME that restores this same table name.
			renamePos := -1
			for _, r := range renameRE.FindAllStringSubmatchIndex(body, -1) {
				newTable := body[r[2]:r[3]]
				restoredTable := body[r[4]:r[5]]
				if newTable == table && restoredTable == table && r[0] > dropPos {
					renamePos = r[0]
					break
				}
			}
			if renamePos == -1 {
				t.Errorf("%s: DROP TABLE %s has no matching ALTER TABLE %s_new RENAME TO %s", path, table, table, table)
				continue
			}

			// A BEGIN must precede the DROP and a COMMIT must follow the
			// RENAME, with no COMMIT in between (i.e. the DROP and RENAME
			// share one open transaction).
			openBegin := -1
			for _, b := range begins {
				if b[0] < dropPos {
					openBegin = b[0]
				}
			}
			if openBegin == -1 {
				t.Errorf("%s: DROP TABLE %s at byte %d is not preceded by an explicit BEGIN; a crash between DROP and RENAME would leave the table dropped-but-not-renamed", path, table, dropPos)
				continue
			}

			closingCommit := -1
			for _, c := range commits {
				if c[0] > renamePos {
					closingCommit = c[0]
					break
				}
			}
			if closingCommit == -1 {
				t.Errorf("%s: ALTER TABLE %s_new RENAME TO %s at byte %d is not followed by a COMMIT", path, table, table, renamePos)
				continue
			}

			// No COMMIT must occur strictly between the open BEGIN and the
			// DROP/RENAME pair (that would close the transaction early,
			// re-exposing the crash window).
			for _, c := range commits {
				if c[0] > openBegin && c[0] < dropPos {
					t.Errorf("%s: transaction opened at byte %d is committed at byte %d before DROP TABLE %s at byte %d — rebuild is not atomic", path, openBegin, c[0], table, dropPos)
					break
				}
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk sqlite migrations: %v", err)
	}
}
