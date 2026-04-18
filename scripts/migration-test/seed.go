// Command seed populates a fresh SQLite database with synthetic,
// production-scale data for the goose migration stress test (P2-TEST-03).
//
// It writes rows directly into the physical tables that every Phase 2
// migration touches: agents, fleet_groups, clients, discovered_clients, jobs,
// job_targets, audit_events, and metric_snapshots. We deliberately operate on
// the raw SQLite schema produced by migration 0001 — the goal is to leave the
// DB in a state a long-lived control-plane would produce, then later run the
// full migration chain over that data and measure behavior.
//
// Usage:
//
//	go run ./scripts/migration-test/seed.go \
//	    -db /tmp/pre-migrate.db \
//	    -agents 100000 -metrics 1000000 -clients 10000 \
//	    -jobs 50000 -audits 500000
//
// The command applies migration 0001 first (via raw SQL embedded below — goose
// cannot be used here because we want to stop at 0001 only), then bulk-inserts
// rows inside a single transaction with PRAGMAs tuned for write throughput.
// Running the real Migrate() afterwards is the job of run.sh, not this
// program.
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// initialSchema0001 is a trimmed copy of the tables touched by later Phase 2
// migrations. We only seed tables whose contents matter for stress-testing
// ALTER/index migrations; fleet_groups is included because agents.fleet_group_id
// references it.
const initialSchema0001 = `
CREATE TABLE IF NOT EXISTS fleet_groups (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    created_at_unix INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS agents (
    id TEXT PRIMARY KEY,
    node_name TEXT NOT NULL,
    fleet_group_id TEXT,
    version TEXT NOT NULL DEFAULT '',
    read_only INTEGER NOT NULL DEFAULT 0,
    last_seen_at_unix INTEGER NOT NULL,
    created_at_unix INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (fleet_group_id) REFERENCES fleet_groups (id)
);

CREATE TABLE IF NOT EXISTS clients (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    secret_ciphertext TEXT NOT NULL,
    user_ad_tag TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    max_tcp_conns INTEGER NOT NULL DEFAULT 0,
    max_unique_ips INTEGER NOT NULL DEFAULT 0,
    data_quota_bytes INTEGER NOT NULL DEFAULT 0,
    expiration_rfc3339 TEXT NOT NULL DEFAULT '',
    created_at_unix INTEGER NOT NULL,
    updated_at_unix INTEGER NOT NULL,
    deleted_at_unix INTEGER
);

CREATE TABLE IF NOT EXISTS jobs (
    id TEXT PRIMARY KEY,
    action TEXT NOT NULL,
    actor_id TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at_unix INTEGER NOT NULL,
    ttl_nanos INTEGER NOT NULL,
    idempotency_key TEXT NOT NULL UNIQUE,
    payload_json TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS job_targets (
    job_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    status TEXT NOT NULL,
    result_text TEXT NOT NULL DEFAULT '',
    result_json TEXT NOT NULL DEFAULT '',
    updated_at_unix INTEGER NOT NULL,
    PRIMARY KEY (job_id, agent_id),
    FOREIGN KEY (job_id) REFERENCES jobs (id)
);

CREATE TABLE IF NOT EXISTS audit_events (
    id TEXT PRIMARY KEY,
    actor_id TEXT NOT NULL,
    action TEXT NOT NULL,
    target_id TEXT NOT NULL,
    created_at_unix INTEGER NOT NULL,
    details_json TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE IF NOT EXISTS metric_snapshots (
    id TEXT PRIMARY KEY,
    agent_id TEXT NOT NULL,
    instance_id TEXT NOT NULL DEFAULT '',
    captured_at_unix INTEGER NOT NULL,
    values_json TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS discovered_clients (
    id TEXT PRIMARY KEY,
    agent_id TEXT NOT NULL,
    client_name TEXT NOT NULL,
    secret TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending_review',
    total_octets INTEGER NOT NULL DEFAULT 0,
    current_connections INTEGER NOT NULL DEFAULT 0,
    active_unique_ips INTEGER NOT NULL DEFAULT 0,
    connection_link TEXT NOT NULL DEFAULT '',
    max_tcp_conns INTEGER NOT NULL DEFAULT 0,
    max_unique_ips INTEGER NOT NULL DEFAULT 0,
    data_quota_bytes INTEGER NOT NULL DEFAULT 0,
    expiration TEXT NOT NULL DEFAULT '',
    discovered_at_unix INTEGER NOT NULL,
    updated_at_unix INTEGER NOT NULL,
    UNIQUE (agent_id, client_name),
    FOREIGN KEY (agent_id) REFERENCES agents (id)
);
`

// chunkSize bounds the number of VALUES rows per INSERT. SQLite tolerates up
// to ~32k parameters by default; at 7 columns per row 2000 rows = 14k params.
const chunkSize = 2000

type counts struct {
	agents, metrics, clients, jobs, audits int
	fleetGroups, discovered                int
}

func main() {
	var (
		dbPath    = flag.String("db", "", "SQLite database path (will be overwritten)")
		agents    = flag.Int("agents", 100_000, "number of agents to seed")
		metrics   = flag.Int("metrics", 1_000_000, "number of metric_snapshots to seed")
		clientsN  = flag.Int("clients", 10_000, "number of clients to seed")
		jobsN     = flag.Int("jobs", 50_000, "number of jobs to seed")
		audits    = flag.Int("audits", 500_000, "number of audit_events to seed")
		fleetN    = flag.Int("fleet-groups", 32, "number of fleet groups to seed")
		discovN   = flag.Int("discovered", 0, "number of discovered_clients to seed")
		statusReport = flag.Bool("status", true, "print timing after each stage")
	)
	flag.Parse()

	if *dbPath == "" {
		log.Fatal("-db is required")
	}
	// Start from a clean slate: the seed must represent a fresh pre-0002 DB,
	// not the union of whatever an old test run left behind.
	_ = os.Remove(*dbPath)

	db, err := sql.Open("sqlite", *dbPath)
	if err != nil {
		log.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	// Tune for bulk insert: synchronous=OFF + WAL speeds seeding by 5-10x.
	// These PRAGMAs are discarded once the process exits — the real Migrate
	// call in run.sh will apply its own PRAGMAs.
	for _, p := range []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = OFF",
		"PRAGMA temp_store = MEMORY",
		"PRAGMA cache_size = -200000", // ~200MB page cache
	} {
		if _, err := db.Exec(p); err != nil {
			log.Fatalf("pragma %q: %v", p, err)
		}
	}

	if _, err := db.Exec(initialSchema0001); err != nil {
		log.Fatalf("apply 0001 schema: %v", err)
	}

	c := counts{
		agents:      *agents,
		metrics:     *metrics,
		clients:     *clientsN,
		jobs:        *jobsN,
		audits:      *audits,
		fleetGroups: *fleetN,
		discovered:  *discovN,
	}

	run := func(name string, fn func() error) {
		t0 := time.Now()
		if err := fn(); err != nil {
			log.Fatalf("seed %s: %v", name, err)
		}
		if *statusReport {
			log.Printf("seeded %-20s in %s", name, time.Since(t0))
		}
	}

	run("fleet_groups", func() error { return seedFleetGroups(db, c.fleetGroups) })
	run("agents", func() error { return seedAgents(db, c.agents, c.fleetGroups) })
	run("clients", func() error { return seedClients(db, c.clients) })
	run("jobs+targets", func() error { return seedJobs(db, c.jobs, c.agents) })
	run("audit_events", func() error { return seedAudits(db, c.audits) })
	run("metric_snapshots", func() error { return seedMetrics(db, c.metrics, c.agents) })
	if c.discovered > 0 {
		run("discovered_clients", func() error { return seedDiscovered(db, c.discovered, c.agents) })
	}

	// Help operators eyeball the seed: print table counts.
	for _, tbl := range []string{"fleet_groups", "agents", "clients", "jobs", "job_targets", "audit_events", "metric_snapshots", "discovered_clients"} {
		var n int64
		if err := db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", tbl)).Scan(&n); err != nil {
			log.Fatalf("count %s: %v", tbl, err)
		}
		log.Printf("  %-20s rows=%d", tbl, n)
	}
}

// txBulk runs insertFn inside a single transaction. Every seed function uses
// this helper; chunked INSERTs inside a single tx is the fastest way to load
// millions of rows into SQLite.
func txBulk(db *sql.DB, insertFn func(*sql.Tx) error) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if err := insertFn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// bulkInsert builds a multi-row INSERT of shape
//
//	INSERT INTO t (c1,c2,...) VALUES (?,?,...),(?,?,...),...
//
// and ships it in `chunkSize`-row batches. rowFn writes one row's worth of
// args to argBuf.
func bulkInsert(tx *sql.Tx, table string, cols []string, total int, rowFn func(i int, args []any)) error {
	if total == 0 {
		return nil
	}
	placeholders := "(" + strings.Repeat("?,", len(cols)-1) + "?)"
	head := fmt.Sprintf("INSERT INTO %s (%s) VALUES ", table, strings.Join(cols, ","))

	for start := 0; start < total; start += chunkSize {
		end := start + chunkSize
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
		if _, err := tx.Exec(sb.String(), args...); err != nil {
			return fmt.Errorf("insert %s [%d:%d]: %w", table, start, end, err)
		}
	}
	return nil
}

func seedFleetGroups(db *sql.DB, n int) error {
	now := time.Now().Unix()
	return txBulk(db, func(tx *sql.Tx) error {
		return bulkInsert(tx, "fleet_groups",
			[]string{"id", "name", "created_at_unix"},
			n,
			func(i int, a []any) {
				a[0] = fmt.Sprintf("fg-%06d", i)
				a[1] = fmt.Sprintf("group-%d", i)
				a[2] = now
			})
	})
}

func seedAgents(db *sql.DB, n, fleetN int) error {
	now := time.Now().Unix()
	return txBulk(db, func(tx *sql.Tx) error {
		return bulkInsert(tx, "agents",
			[]string{"id", "node_name", "fleet_group_id", "version", "read_only", "last_seen_at_unix", "created_at_unix"},
			n,
			func(i int, a []any) {
				a[0] = fmt.Sprintf("agent-%08d", i)
				a[1] = fmt.Sprintf("node-%d", i)
				// Distribute agents across fleet groups. A subset has no group
				// to exercise the nullable FK path that migration 0008 indexes.
				if fleetN > 0 && i%5 != 0 {
					a[2] = fmt.Sprintf("fg-%06d", i%fleetN)
				} else {
					a[2] = nil
				}
				a[3] = "1.2.3"
				a[4] = 0
				a[5] = now - int64(i%3600)
				a[6] = now - int64(i%86400)
			})
	})
}

func seedClients(db *sql.DB, n int) error {
	now := time.Now().Unix()
	return txBulk(db, func(tx *sql.Tx) error {
		return bulkInsert(tx, "clients",
			[]string{"id", "name", "secret_ciphertext", "user_ad_tag", "enabled",
				"max_tcp_conns", "max_unique_ips", "data_quota_bytes",
				"expiration_rfc3339", "created_at_unix", "updated_at_unix"},
			n,
			func(i int, a []any) {
				a[0] = fmt.Sprintf("client-%08d", i)
				a[1] = fmt.Sprintf("client-%d", i)
				a[2] = "deadbeef"
				a[3] = ""
				a[4] = 1
				a[5] = 1000
				a[6] = 100
				a[7] = int64(1 << 30)
				a[8] = ""
				a[9] = now - int64(i%86400)
				a[10] = now
			})
	})
}

func seedJobs(db *sql.DB, n, agentN int) error {
	now := time.Now().Unix()
	err := txBulk(db, func(tx *sql.Tx) error {
		return bulkInsert(tx, "jobs",
			[]string{"id", "action", "actor_id", "status", "created_at_unix", "ttl_nanos", "idempotency_key", "payload_json"},
			n,
			func(i int, a []any) {
				a[0] = fmt.Sprintf("job-%08d", i)
				a[1] = "rollout"
				a[2] = "system"
				// Status distribution must include 'queued' so migration
				// 0008's idx_jobs_status has non-trivial selectivity.
				switch i % 4 {
				case 0:
					a[3] = "queued"
				case 1:
					a[3] = "running"
				case 2:
					a[3] = "succeeded"
				default:
					a[3] = "failed"
				}
				a[4] = now - int64(i%86400)
				a[5] = int64(time.Minute)
				a[6] = fmt.Sprintf("idemp-%08d", i)
				a[7] = "{}"
			})
	})
	if err != nil {
		return err
	}
	if agentN == 0 {
		return nil
	}
	// One target per job keeps cardinality predictable.
	return txBulk(db, func(tx *sql.Tx) error {
		return bulkInsert(tx, "job_targets",
			[]string{"job_id", "agent_id", "status", "result_text", "result_json", "updated_at_unix"},
			n,
			func(i int, a []any) {
				a[0] = fmt.Sprintf("job-%08d", i)
				a[1] = fmt.Sprintf("agent-%08d", i%agentN)
				a[2] = "pending"
				a[3] = ""
				a[4] = ""
				a[5] = now
			})
	})
}

func seedAudits(db *sql.DB, n int) error {
	now := time.Now().Unix()
	return txBulk(db, func(tx *sql.Tx) error {
		return bulkInsert(tx, "audit_events",
			[]string{"id", "actor_id", "action", "target_id", "created_at_unix", "details_json"},
			n,
			func(i int, a []any) {
				a[0] = fmt.Sprintf("audit-%09d", i)
				a[1] = fmt.Sprintf("user-%d", i%500)
				a[2] = "client.update"
				a[3] = fmt.Sprintf("client-%08d", i%10_000)
				a[4] = now - int64(i%86400*30)
				// Realistic details payload — migration 0011 renames the
				// column so we want the data to survive the ALTER.
				a[5] = `{"ip":"10.0.0.1","ua":"panvex-cli/1.0"}`
			})
	})
}

func seedMetrics(db *sql.DB, n, agentN int) error {
	if agentN == 0 {
		return fmt.Errorf("metrics require at least one agent")
	}
	return txBulk(db, func(tx *sql.Tx) error {
		return bulkInsert(tx, "metric_snapshots",
			[]string{"id", "agent_id", "instance_id", "captured_at_unix", "values_json"},
			n,
			func(i int, a []any) {
				a[0] = fmt.Sprintf("metric-%010d", i)
				a[1] = fmt.Sprintf("agent-%08d", i%agentN)
				a[2] = ""
				// Spread across a 7-day window so retention index 0008 sees
				// realistic selectivity.
				a[3] = time.Now().Add(-time.Duration(i%(7*24*3600)) * time.Second).Unix()
				a[4] = `{"cpu":0.5,"mem":0.3,"conns":1234}`
			})
	})
}

func seedDiscovered(db *sql.DB, n, agentN int) error {
	now := time.Now().Unix()
	return txBulk(db, func(tx *sql.Tx) error {
		return bulkInsert(tx, "discovered_clients",
			[]string{"id", "agent_id", "client_name", "secret", "status",
				"total_octets", "current_connections", "active_unique_ips",
				"connection_link", "max_tcp_conns", "max_unique_ips",
				"data_quota_bytes", "expiration", "discovered_at_unix", "updated_at_unix"},
			n,
			func(i int, a []any) {
				a[0] = fmt.Sprintf("disc-%08d", i)
				// Migration 0010 adds UNIQUE(agent_id, client_name) where
				// status='pending_review' — we need distinct (agent,name)
				// pairs within the pending bucket. The (i, i) mapping
				// guarantees uniqueness.
				a[1] = fmt.Sprintf("agent-%08d", i%agentN)
				a[2] = fmt.Sprintf("discovered-%d", i)
				a[3] = "secret"
				a[4] = "pending_review"
				a[5] = 0
				a[6] = 0
				a[7] = 0
				a[8] = ""
				a[9] = 0
				a[10] = 0
				a[11] = 0
				a[12] = ""
				a[13] = now
				a[14] = now
			})
	})
}
