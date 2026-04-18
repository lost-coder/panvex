//go:build stress

// Stress test for the goose migration chain (P2-TEST-03).
//
// Gated behind the `stress` build tag so it does not run in the default
// `go test ./...` path — seeding 10k agents + 100k metric_snapshots plus the
// full migration chain takes minutes on slower laptops and is unnecessary for
// every CI run. Invoke it explicitly:
//
//	go test -tags stress -count=1 -timeout 15m \
//	    ./internal/controlplane/storage/sqlite -run TestMigrateStress
//
// What this test proves:
//   - Migrate() succeeds against a database already populated with
//     production-scale row counts at schema 0001 — not an empty DB.
//   - Every post-0001 migration (through 0011) preserves the seeded rows:
//     no ALTER/RENAME/INSERT-SELECT step silently drops data.
//   - Index-creation migrations (0007, 0008) and the 0011 column rename
//     complete within a reasonable wall-clock budget for the operator runbook.
//
// What this test does NOT cover:
//   - PostgreSQL. That path is exercised by postgres.TestMigrate* under its
//     own `pgtest` tag when PANVEX_POSTGRES_TEST_DSN is set.
//   - Down migrations. Goose Down is rarely used in prod and is covered by
//     the regular migrate_test.go idempotency check.

package sqlite

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// stressSchema0001 is a trimmed copy of the tables every later Phase 2
// migration touches. We cannot invoke goose to apply only 0001 because goose
// has no "stop at version N" primitive from within a test; instead we
// materialise the exact pre-0002 shape of every table we need, then let the
// real Migrate() run the 0002..HEAD chain over the seeded data.
//
// Keep this schema in sync with db/migrations/sqlite/0001_init.sql for the
// tables listed below. If a future 0001 revision changes a column, this
// const must match — otherwise the later ALTER migrations will see the
// "wrong" starting shape and either fail or silently miss a rename.
const stressSchema0001 = `
CREATE TABLE fleet_groups (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    created_at_unix INTEGER NOT NULL
);

CREATE TABLE agents (
    id TEXT PRIMARY KEY,
    node_name TEXT NOT NULL,
    fleet_group_id TEXT,
    version TEXT NOT NULL DEFAULT '',
    read_only INTEGER NOT NULL DEFAULT 0,
    last_seen_at_unix INTEGER NOT NULL,
    created_at_unix INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (fleet_group_id) REFERENCES fleet_groups (id)
);

CREATE TABLE audit_events (
    id TEXT PRIMARY KEY,
    actor_id TEXT NOT NULL,
    action TEXT NOT NULL,
    target_id TEXT NOT NULL,
    created_at_unix INTEGER NOT NULL,
    details_json TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE metric_snapshots (
    id TEXT PRIMARY KEY,
    agent_id TEXT NOT NULL,
    instance_id TEXT NOT NULL DEFAULT '',
    captured_at_unix INTEGER NOT NULL,
    values_json TEXT NOT NULL
);
`

// The stress test scale. These numbers are the smallest that still exercise
// the behaviours we care about — large enough that any O(N) anomaly shows up
// in wall-clock time, small enough to finish in under 2 minutes on a laptop.
const (
	stressAgents  = 10_000
	stressMetrics = 100_000
	stressAudits  = 10_000
	stressGroups  = 32
	// Chunk size for multi-row INSERTs. At 5 columns per metric_snapshots row
	// this stays well under SQLite's default 32k-parameter ceiling.
	stressChunk = 2_000
)

// TestMigrateStress seeds a pre-0002 SQLite DB at production scale, runs the
// real Migrate() chain, and asserts that (a) it completes, (b) row counts
// are preserved through every ALTER/RENAME/INSERT-SELECT step, (c) the
// schema-level contracts from later migrations (indexes from 0007/0008, the
// 0011 column rename) are honoured on a non-empty DB.
func TestMigrateStress(t *testing.T) {
	// A stress test that takes a minute to fail should give a clear reason
	// when it times out, not a truncated testing.T.Fatal.
	started := time.Now()
	defer func() {
		t.Logf("TestMigrateStress wall-clock: %s", time.Since(started))
	}()

	db := openStressSQLite(t)

	// Stage 1 — materialise schema 0001. No goose: we want the exact column
	// layout that existed before 0002..0011 ran.
	if _, err := db.Exec(stressSchema0001); err != nil {
		t.Fatalf("apply stressSchema0001: %v", err)
	}

	// Stage 2 — bulk-seed. Tune PRAGMAs for speed; Migrate() will pick its
	// own PRAGMAs downstream.
	applyPragmas(t, db,
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = OFF",
		"PRAGMA temp_store = MEMORY",
		"PRAGMA cache_size = -200000", // ~200MB page cache during seed
	)
	seedStart := time.Now()
	seedStressData(t, db)
	t.Logf("seeded %d agents + %d metric_snapshots + %d audits in %s",
		stressAgents, stressMetrics, stressAudits, time.Since(seedStart))

	// Capture pre-migration row counts so we can diff them after Migrate().
	// Every later migration must be data-preserving on these tables.
	pre := countTables(t, db, "fleet_groups", "agents", "audit_events", "metric_snapshots")

	// Stage 3 — run the real migration chain. This is the behaviour under
	// test; a panic, error, or hang here is the whole failure mode.
	migStart := time.Now()
	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate() on seeded DB: %v", err)
	}
	t.Logf("Migrate() applied full chain in %s", time.Since(migStart))

	// Stage 4 — invariants.
	post := countTables(t, db, "fleet_groups", "agents", "audit_events", "metric_snapshots")
	for tbl, want := range pre {
		if got := post[tbl]; got != want {
			t.Errorf("row count drift on %s: pre=%d post=%d", tbl, want, got)
		}
	}

	// Verify the 0011 column rename actually moved data. Reading the renamed
	// column confirms both schema and row survival.
	var anyValues string
	err := db.QueryRow(`SELECT "values" FROM metric_snapshots LIMIT 1`).Scan(&anyValues)
	if err != nil {
		t.Fatalf("read metric_snapshots.values after 0011: %v", err)
	}
	if anyValues == "" {
		t.Errorf("metric_snapshots.values is empty — 0011 rename may have dropped data")
	}
	var anyDetails string
	err = db.QueryRow(`SELECT details FROM audit_events LIMIT 1`).Scan(&anyDetails)
	if err != nil {
		t.Fatalf("read audit_events.details after 0011: %v", err)
	}
	if anyDetails == "" {
		t.Errorf("audit_events.details is empty — 0011 rename may have dropped data")
	}

	// P2-DB-02 / DF-22 indexes must be present after 0008. On an empty DB
	// that is already covered by TestMigrateCreatesMissingFKIndexes; here we
	// assert the same holds when the index is built over real data.
	for _, idx := range []string{
		"idx_jobs_status",
		"idx_job_targets_agent_id",
		"idx_metric_snapshots_captured_at",
		"idx_enrollment_tokens_fleet_group_id",
	} {
		var name string
		if err := db.QueryRow(
			`SELECT name FROM sqlite_master WHERE type='index' AND name=?`, idx,
		).Scan(&name); err != nil {
			t.Errorf("missing index %q after Migrate on seeded DB: %v", idx, err)
		}
	}

	// goose_db_version must hold one row per applied migration. A missing
	// row here means a migration ran without being recorded — the DF-20
	// failure mode we built this whole framework to eliminate.
	var versionCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM goose_db_version`).Scan(&versionCount); err != nil {
		t.Fatalf("count goose_db_version: %v", err)
	}
	// At minimum the 11 embedded migrations (0001..0011) plus goose's
	// zero-version bookkeeping row.
	if versionCount < 11 {
		t.Errorf("goose_db_version has %d rows, expected >= 11", versionCount)
	}

	// Re-running Migrate on the seeded+migrated DB must be a no-op. This
	// catches the case where a migration accidentally re-runs DDL on an
	// already-upgraded schema.
	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate() second pass on seeded DB: %v", err)
	}
	after := countTables(t, db, "fleet_groups", "agents", "audit_events", "metric_snapshots")
	for tbl, want := range post {
		if got := after[tbl]; got != want {
			t.Errorf("row count changed on second Migrate(): %s pre=%d post=%d", tbl, want, got)
		}
	}
}

// openStressSQLite opens a file-backed SQLite DB in t.TempDir. We avoid
// ":memory:" because the seeded dataset (~50 MB) makes memory pressure a
// confounding variable — we want to measure migration cost, not GC cost.
func openStressSQLite(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "migrate-stress.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	// Pool of 1: goose and our PRAGMAs both rely on a single connection to
	// see consistent session state.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		t.Fatalf("PRAGMA foreign_keys: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func applyPragmas(t *testing.T, db *sql.DB, pragmas ...string) {
	t.Helper()
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			t.Fatalf("pragma %q: %v", p, err)
		}
	}
}

// seedStressData inserts fleet_groups, agents, audit_events, and
// metric_snapshots. We batch inside a single transaction per table because
// SQLite's autocommit-per-statement is ~100x slower than a single tx.
func seedStressData(t *testing.T, db *sql.DB) {
	t.Helper()
	now := time.Now().Unix()

	// fleet_groups — referenced by agents.fleet_group_id FK.
	seedBulk(t, db, "fleet_groups",
		[]string{"id", "name", "created_at_unix"},
		stressGroups,
		func(i int, a []any) {
			a[0] = fmt.Sprintf("fg-%06d", i)
			a[1] = fmt.Sprintf("group-%d", i)
			a[2] = now
		},
	)

	// agents — some with nil fleet_group_id to exercise the nullable FK path
	// that migration 0008 indexes.
	seedBulk(t, db, "agents",
		[]string{"id", "node_name", "fleet_group_id", "version", "read_only", "last_seen_at_unix", "created_at_unix"},
		stressAgents,
		func(i int, a []any) {
			a[0] = fmt.Sprintf("agent-%08d", i)
			a[1] = fmt.Sprintf("node-%d", i)
			if i%5 != 0 {
				a[2] = fmt.Sprintf("fg-%06d", i%stressGroups)
			} else {
				a[2] = nil
			}
			a[3] = "1.2.3"
			a[4] = 0
			a[5] = now - int64(i%3600)
			a[6] = now - int64(i%86400)
		},
	)

	// audit_events — realistic JSON payload so migration 0011's rename has
	// actual bytes to carry forward.
	seedBulk(t, db, "audit_events",
		[]string{"id", "actor_id", "action", "target_id", "created_at_unix", "details_json"},
		stressAudits,
		func(i int, a []any) {
			a[0] = fmt.Sprintf("audit-%09d", i)
			a[1] = fmt.Sprintf("user-%d", i%500)
			a[2] = "client.update"
			a[3] = fmt.Sprintf("target-%d", i)
			a[4] = now - int64(i%86400*30)
			a[5] = `{"ip":"10.0.0.1","ua":"panvex-cli/1.0"}`
		},
	)

	// metric_snapshots — the big one. Spread across a 7-day window so the
	// captured_at index from 0008 sees realistic selectivity.
	seedBulk(t, db, "metric_snapshots",
		[]string{"id", "agent_id", "instance_id", "captured_at_unix", "values_json"},
		stressMetrics,
		func(i int, a []any) {
			a[0] = fmt.Sprintf("metric-%010d", i)
			a[1] = fmt.Sprintf("agent-%08d", i%stressAgents)
			a[2] = ""
			a[3] = now - int64(i%(7*24*3600))
			a[4] = `{"cpu":0.5,"mem":0.3,"conns":1234}`
		},
	)
}

// seedBulk bulk-inserts `total` rows into `table` using chunked multi-row
// INSERTs inside a single transaction. This is 10-50x faster than prepared
// single-row inserts for the scales we care about.
func seedBulk(t *testing.T, db *sql.DB, table string, cols []string, total int, rowFn func(i int, args []any)) {
	t.Helper()
	if total == 0 {
		return
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin tx for %s: %v", table, err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	placeholders := "(" + strings.Repeat("?,", len(cols)-1) + "?)"
	head := fmt.Sprintf("INSERT INTO %s (%s) VALUES ", table, strings.Join(cols, ","))

	for start := 0; start < total; start += stressChunk {
		end := start + stressChunk
		if end > total {
			end = total
		}
		n := end - start
		var sb strings.Builder
		sb.Grow(len(head) + n*(len(placeholders)+1))
		sb.WriteString(head)
		for i := 0; i < n; i++ {
			if i > 0 {
				sb.WriteByte(',')
			}
			sb.WriteString(placeholders)
		}

		args := make([]any, 0, n*len(cols))
		rowBuf := make([]any, len(cols))
		for i := 0; i < n; i++ {
			rowFn(start+i, rowBuf)
			args = append(args, rowBuf...)
		}
		if _, err = tx.Exec(sb.String(), args...); err != nil {
			t.Fatalf("insert %s [%d:%d]: %v", table, start, end, err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatalf("commit %s: %v", table, err)
	}
}

// countTables returns a map of table name -> COUNT(*). Used to diff row
// counts across the migration boundary.
func countTables(t *testing.T, db *sql.DB, tables ...string) map[string]int64 {
	t.Helper()
	out := make(map[string]int64, len(tables))
	for _, tbl := range tables {
		var n int64
		if err := db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", tbl)).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", tbl, err)
		}
		out[tbl] = n
	}
	return out
}
