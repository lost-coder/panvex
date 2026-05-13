package postgres

// EXPLAIN ANALYZE plan-shape regression test.
//
// Goal: lock in the query plan for the 5-10 hottest read queries so a
// future migration that drops an index, adds a column, or rewrites a
// query into a sequential scan fails CI loudly. The test does NOT save
// snapshots to disk — it asserts structural invariants per query
// (index name expected, no Seq Scan on indexed tables) which keeps the
// signal-to-noise ratio high without snapshot churn.
//
// Skipping convention: when PANVEX_POSTGRES_TEST_DSN is unset the test
// skips, matching the schema-sync job convention. CI wires the DSN into
// the schema-sync job (see .github/workflows/ci.yml) so this test runs
// alongside the existing migration drift gate.
//
// ─── Adding a new query ────────────────────────────────────────────────
//
// 1. Pick a representative read query from one of the storage files
//    (agents.go, clients.go, jobs.go, audit.go, telemetry.go,
//    timeseries.go). It should be a real hot path — paginated lists,
//    cursor reads, time-windowed aggregates.
//
// 2. Append a hotQuery{} entry to hotQueries() below. Required fields:
//      - name: short, references the Go function (e.g. "ListJobsCursor").
//      - sql: the literal SQL — copy from the .go file verbatim, NOT a
//        re-implementation. Plan drift between code and test is the
//        whole bug we are guarding against.
//      - args: []any with realistic values. Seed fixtures in
//        seedExplainFixtures (below) if the query needs specific rows.
//      - assertions: pick from the helper set:
//          * mustUseIndex("idx_name")        — index appears in plan.
//          * mustNotSeqScan("table_name")    — Seq Scan on table fails.
//          * costCeiling(<float>)            — total cost ≤ ceiling
//                                              (use sparingly; data
//                                              sizes are tiny in CI).
//
// 3. If the query needs more than the seeded one-agent-one-client-one-job
//    shape, extend seedExplainFixtures. Be defensive — Postgres skips
//    indexes when the table is small enough that a Seq Scan is faster.
//    Most hot queries still pick the index even on tiny tables because
//    the planner sees the cardinality drop from the WHERE clause; if a
//    query unexpectedly seq-scans, seed enough rows that an index scan
//    is the cheaper option (~200+ rows is usually enough).
//
// ─── What this test does NOT cover ─────────────────────────────────────
//
// - Production-scale data shapes. Plan choice can flip on a 10M-row
//   table that is happy with an index scan on a 50-row test table. We
//   prefer false negatives (missed regression in CI, caught in load
//   bench) over false positives (flaky CI). For volume-sensitive plan
//   shapes use the load harness in internal/loadtest.
//
// - Cost regressions (e.g. plan switched from one index to a different
//   one with worse selectivity). Add an explicit mustUseIndex on the
//   index you require.
//
// - Lock contention, autovacuum behaviour, or anything observable only
//   under concurrent load.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	// register the pgx driver under "pgx" for database/sql
	_ "github.com/jackc/pgx/v5/stdlib"
)

// hotQuery describes one read query whose plan shape we want to lock
// in. Each entry is exercised by EXPLAIN (FORMAT JSON, ANALYZE, BUFFERS)
// and the resulting plan tree is checked against assertions.
type hotQuery struct {
	name       string
	sql        string
	args       []any
	assertions []planAssertion
}

// planAssertion is a single structural check applied to a parsed plan
// tree. Helpers below build these — tests should never instantiate one
// by hand.
type planAssertion struct {
	desc  string
	check func(t *testing.T, name string, plan map[string]any)
}

// TestExplainAnalyze_HotQueries asserts plan-shape invariants for the
// hottest read queries on the postgres backend. See file-level docstring
// for how to add new queries.
func TestExplainAnalyze_HotQueries(t *testing.T) {
	dsn := os.Getenv("PANVEX_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("PANVEX_POSTGRES_TEST_DSN not set; matches schema-sync convention")
	}

	ctx := t.Context()

	store, err := Open(dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := resetForTest(ctx, store); err != nil {
		t.Fatalf("resetForTest: %v", err)
	}

	fixtures, err := seedExplainFixtures(ctx, store.sqlDB)
	if err != nil {
		t.Fatalf("seedExplainFixtures: %v", err)
	}

	// ANALYZE so the planner has fresh statistics. Without it the small
	// seeded tables can still have stale-or-missing pg_statistic rows
	// (especially right after CREATE INDEX) and the planner falls back
	// to defaults that prefer Seq Scan even when an index is cheaper.
	if _, err := store.sqlDB.ExecContext(ctx, "ANALYZE"); err != nil {
		t.Fatalf("ANALYZE: %v", err)
	}

	// Disable Seq Scan so the planner picks index paths even on the
	// tiny seeded tables, where a single-row heap scan would otherwise
	// be cost-optimal. This makes the `mustNotSeqScan` assertions check
	// the "is there a usable index?" invariant — which is what production
	// will need at scale — rather than the cost crossover on a 1-row
	// fixture. A missing-index regression still surfaces because the
	// planner falls back to Seq Scan even with this disabled.
	if _, err := store.sqlDB.ExecContext(ctx, "SET enable_seqscan = off"); err != nil {
		t.Fatalf("SET enable_seqscan = off: %v", err)
	}
	t.Cleanup(func() {
		_, _ = store.sqlDB.ExecContext(ctx, "RESET enable_seqscan")
	})

	for _, q := range hotQueries(fixtures) {
		q := q
		t.Run(q.name, func(t *testing.T) {
			plan, err := explainAnalyze(ctx, store.sqlDB, q.sql, q.args...)
			if err != nil {
				t.Fatalf("EXPLAIN ANALYZE %s: %v", q.name, err)
			}
			for _, a := range q.assertions {
				a.check(t, q.name, plan)
			}
		})
	}
}

// explainFixtures bundles the seeded IDs the query catalog needs.
type explainFixtures struct {
	AgentID         string
	OtherAgentID    string
	ClientID        string
	JobID           string
	JobCreatedAt    time.Time
	WindowStart     time.Time
	WindowEnd       time.Time
	AuditAfterID    string
	AuditAfterTime  time.Time
	JobAfterID      string
	JobAfterTime    time.Time
}

// seedExplainFixtures populates the bare minimum of rows the EXPLAIN
// queries need. Note: most hot queries pick their index even on tiny
// tables because the WHERE clause is highly selective. Don't seed
// thousands of rows just for plan shape — keep the test fast.
func seedExplainFixtures(ctx context.Context, db *sql.DB) (explainFixtures, error) {
	now := time.Date(2026, time.May, 7, 12, 0, 0, 0, time.UTC)
	fx := explainFixtures{
		AgentID:        "agent-explain-1",
		OtherAgentID:   "agent-explain-2",
		ClientID:       "client-explain-1",
		JobID:          "job-explain-1",
		JobCreatedAt:   now.Add(-1 * time.Hour),
		WindowStart:    now.Add(-24 * time.Hour),
		WindowEnd:      now,
		AuditAfterID:   "audit-explain-cursor",
		AuditAfterTime: now.Add(-30 * time.Minute),
		JobAfterID:     "job-explain-cursor",
		JobAfterTime:   now.Add(-30 * time.Minute),
	}

	// agents — required by FK on telemt_runtime_events / job_targets.
	for _, id := range []string{fx.AgentID, fx.OtherAgentID} {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO agents (id, node_name, version, read_only, last_seen_at)
			VALUES ($1, $1, '0.0.0', false, $2)
			ON CONFLICT (id) DO NOTHING
		`, id, now); err != nil {
			return fx, fmt.Errorf("seed agents: %w", err)
		}
	}

	// clients — bare row.
	if _, err := db.ExecContext(ctx, `
		INSERT INTO clients (
			id, name, secret_ciphertext, user_ad_tag, enabled,
			max_tcp_conns, max_unique_ips, data_quota_bytes,
			expiration_rfc3339, created_at, updated_at
		)
		VALUES ($1, $1, ''::bytea, '', true, 0, 0, 0, '', $2, $2)
		ON CONFLICT (id) DO NOTHING
	`, fx.ClientID, now); err != nil {
		return fx, fmt.Errorf("seed clients: %w", err)
	}

	// client_assignments / client_deployments — exercise the per-client
	// fan-out queries.
	if _, err := db.ExecContext(ctx, `
		INSERT INTO client_assignments (id, client_id, target_type, agent_id, created_at)
		VALUES ('ca-1', $1, 'agent', $2, $3)
		ON CONFLICT (id) DO NOTHING
	`, fx.ClientID, fx.AgentID, now); err != nil {
		return fx, fmt.Errorf("seed client_assignments: %w", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO client_deployments (
			client_id, agent_id, desired_operation, status,
			last_error, connection_links, last_applied_at, updated_at
		)
		VALUES ($1, $2, 'apply', 'queued', '', '[]'::jsonb, NULL, $3)
		ON CONFLICT (client_id, agent_id) DO NOTHING
	`, fx.ClientID, fx.AgentID, now); err != nil {
		return fx, fmt.Errorf("seed client_deployments: %w", err)
	}

	// jobs + job_targets — exercise the cursor pagination plan.
	for i, id := range []string{fx.JobID, fx.JobAfterID} {
		ts := fx.JobCreatedAt.Add(time.Duration(i) * time.Minute)
		if _, err := db.ExecContext(ctx, `
			INSERT INTO jobs (id, action, idempotency_key, actor_id, status, created_at, ttl_nanos, payload_json)
			VALUES ($1, 'noop', $1, 'system', 'queued', $2, 0, '')
			ON CONFLICT (id) DO NOTHING
		`, id, ts); err != nil {
			return fx, fmt.Errorf("seed jobs: %w", err)
		}
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO job_targets (job_id, agent_id, status, result_text, result_json, updated_at)
		VALUES ($1, $2, 'queued', '', '', $3)
		ON CONFLICT (job_id, agent_id) DO NOTHING
	`, fx.JobID, fx.AgentID, now); err != nil {
		return fx, fmt.Errorf("seed job_targets: %w", err)
	}

	// audit_events — two rows, one of which is the cursor anchor.
	for i, id := range []string{"audit-explain-1", fx.AuditAfterID} {
		ts := fx.AuditAfterTime.Add(time.Duration(i) * time.Minute)
		if _, err := db.ExecContext(ctx, `
			INSERT INTO audit_events (id, actor_id, action, target_id, details, created_at)
			VALUES ($1, 'system', 'noop', '', '{}'::jsonb, $2)
			ON CONFLICT (id) DO NOTHING
		`, id, ts); err != nil {
			return fx, fmt.Errorf("seed audit_events: %w", err)
		}
	}

	// telemt_runtime_events — agent-scoped event timeline.
	if _, err := db.ExecContext(ctx, `
		INSERT INTO telemt_runtime_events (agent_id, sequence, observed_at, timestamp_at, event_type, context, severity)
		VALUES ($1, 1, $2, $2, 'noop', '', 'info')
		ON CONFLICT (agent_id, sequence) DO NOTHING
	`, fx.AgentID, now); err != nil {
		return fx, fmt.Errorf("seed telemt_runtime_events: %w", err)
	}

	// ts_server_load — agent-scoped time-windowed metrics.
	if _, err := db.ExecContext(ctx, `
		INSERT INTO ts_server_load (agent_id, captured_at)
		VALUES ($1, $2)
		ON CONFLICT (agent_id, captured_at) DO NOTHING
	`, fx.AgentID, now.Add(-30*time.Minute)); err != nil {
		return fx, fmt.Errorf("seed ts_server_load: %w", err)
	}

	// client_ip_history — per-client per-IP fold.
	if _, err := db.ExecContext(ctx, `
		INSERT INTO client_ip_history (agent_id, client_id, ip_address, first_seen, last_seen)
		VALUES ($1, $2, '10.0.0.1', $3, $4)
		ON CONFLICT (agent_id, client_id, ip_address) DO NOTHING
	`, fx.AgentID, fx.ClientID, fx.WindowStart, now); err != nil {
		return fx, fmt.Errorf("seed client_ip_history: %w", err)
	}

	return fx, nil
}

// hotQueries returns the catalog. Keep entries in storage-file order so
// reviewers can walk file → query in one pass.
func hotQueries(fx explainFixtures) []hotQuery {
	return []hotQuery{
		// ── agents.go ──────────────────────────────────────────────
		// ListAgents: full-table scan ordered by last_seen_at, id. We
		// don't enforce a specific operator here — on a tiny test
		// table the planner may legitimately Seq Scan + Sort. We do
		// enforce that the final cost stays sane.
		{
			name: "ListAgents",
			sql: `SELECT id, node_name, fleet_group_id, version, read_only,
                  last_seen_at, cert_issued_at, cert_expires_at
              FROM agents
              ORDER BY last_seen_at ASC, id ASC`,
			assertions: []planAssertion{
				costCeiling(50.0), // guideline: tiny table, must stay cheap
			},
		},

		// ── jobs.go ────────────────────────────────────────────────
		// ListJobsCursor first page: no WHERE, ORDER BY created_at
		// DESC, id DESC LIMIT N. Should ride idx_jobs_created_at on
		// any non-trivial table; on the tiny seeded table the planner
		// may still pick Seq Scan + Sort, so we only enforce cost.
		{
			name: "ListJobsCursor_FirstPage",
			sql: `SELECT id, action, idempotency_key, actor_id, status, created_at, ttl_nanos, payload_json
              FROM jobs
              ORDER BY created_at DESC, id DESC
              LIMIT $1`,
			args: []any{int64(51)},
			assertions: []planAssertion{
				costCeiling(50.0),
			},
		},
		// ListJobsCursor follow-up page with the keyset predicate.
		// Postgres should evaluate (created_at, id) < ($1, $2) using
		// the PK / created_at index — never a Seq Scan even at scale.
		{
			name: "ListJobsCursor_NextPage",
			sql: `SELECT id, action, idempotency_key, actor_id, status, created_at, ttl_nanos, payload_json
              FROM jobs
              WHERE (created_at, id) < ($1, $2)
              ORDER BY created_at DESC, id DESC
              LIMIT $3`,
			args: []any{fx.JobAfterTime.UTC(), fx.JobAfterID, int64(51)},
			assertions: []planAssertion{
				// jobs grows fast in production; if this regresses to
				// Seq Scan once the table is large enough, the load
				// bench will scream — but the test should still
				// guard the structural shape today.
				costCeiling(50.0),
			},
		},
		// ListJobTargets: WHERE job_id = $1 — must hit the (job_id,
		// agent_id) PK, not Seq Scan once production has thousands of
		// targets per job.
		{
			name: "ListJobTargets",
			sql: `SELECT job_id, agent_id, status, result_text, result_json, updated_at
              FROM job_targets
              WHERE job_id = $1
              ORDER BY agent_id`,
			args: []any{fx.JobID},
			assertions: []planAssertion{
				mustNotSeqScan("job_targets"),
			},
		},

		// ── audit.go ───────────────────────────────────────────────
		// ListAuditEventsCursor first page: same shape as jobs cursor
		// but on audit_events. idx_audit_events_created_at exists for
		// exactly this access pattern (P2-DB-02, R-Q-03).
		{
			name: "ListAuditEventsCursor_FirstPage",
			sql: `SELECT id, actor_id, action, target_id, details, created_at
              FROM audit_events
              ORDER BY created_at DESC, id DESC
              LIMIT $1`,
			args: []any{int64(51)},
			assertions: []planAssertion{
				costCeiling(50.0),
			},
		},
		// Cursor variant: tuple-comparison on (created_at, id).
		{
			name: "ListAuditEventsCursor_NextPage",
			sql: `SELECT id, actor_id, action, target_id, details, created_at
              FROM audit_events
              WHERE (created_at, id) < ($1, $2)
              ORDER BY created_at DESC, id DESC
              LIMIT $3`,
			args: []any{fx.AuditAfterTime.UTC(), fx.AuditAfterID, int64(51)},
			assertions: []planAssertion{
				costCeiling(50.0),
			},
		},

		// ── clients.go ─────────────────────────────────────────────
		// ListClientAssignments: WHERE client_id = $1. Backed by
		// idx_client_assignments_client_id from 0007_indexes.sql.
		{
			name: "ListClientAssignments",
			sql: `SELECT id, client_id, target_type, fleet_group_id, agent_id, created_at
              FROM client_assignments
              WHERE client_id = $1
              ORDER BY created_at, id`,
			args: []any{fx.ClientID},
			assertions: []planAssertion{
				mustNotSeqScan("client_assignments"),
			},
		},
		// ListClientDeployments: WHERE client_id = $1. Backed by
		// idx_client_deployments_client_id (also 0007).
		{
			name: "ListClientDeployments",
			sql: `SELECT client_id, agent_id, desired_operation, status, last_error, connection_links, last_applied_at, updated_at
              FROM client_deployments
              WHERE client_id = $1
              ORDER BY agent_id`,
			args: []any{fx.ClientID},
			assertions: []planAssertion{
				mustNotSeqScan("client_deployments"),
			},
		},

		// ── telemetry.go ───────────────────────────────────────────
		// ListTelemetryRuntimeEvents: agent-scoped event timeline.
		// Composite PK (agent_id, sequence) supports the prefix
		// lookup; ORDER BY timestamp_at DESC may force a Sort, which
		// is acceptable on this query.
		{
			name: "ListTelemetryRuntimeEvents",
			sql: `SELECT agent_id, sequence, observed_at, timestamp_at, event_type, context, severity
              FROM telemt_runtime_events
              WHERE agent_id = $1
              ORDER BY timestamp_at DESC, sequence DESC
              LIMIT $2`,
			args: []any{fx.AgentID, int64(100)},
			assertions: []planAssertion{
				mustNotSeqScan("telemt_runtime_events"),
			},
		},

		// ── timeseries.go ──────────────────────────────────────────
		// AggregateClientIPHistory: per-client time-window aggregate.
		// Backed by idx_client_ip_client (client_id, last_seen DESC).
		{
			name: "AggregateClientIPHistory",
			sql: `SELECT ip_address, MIN(first_seen) AS first_seen, MAX(last_seen) AS last_seen
              FROM client_ip_history
              WHERE client_id = $1 AND last_seen >= $2 AND first_seen <= $3
              GROUP BY ip_address
              ORDER BY last_seen DESC
              LIMIT $4`,
			args: []any{fx.ClientID, fx.WindowStart, fx.WindowEnd, int64(1024)},
			assertions: []planAssertion{
				mustNotSeqScan("client_ip_history"),
			},
		},
	}
}

// ─── EXPLAIN runner + plan walker ──────────────────────────────────────

// explainAnalyze runs EXPLAIN (FORMAT JSON, ANALYZE, BUFFERS) and
// returns the parsed root Plan node (the value of the "Plan" key in
// the first array element). The caller walks it via planNodes().
func explainAnalyze(ctx context.Context, db *sql.DB, sql string, args ...any) (map[string]any, error) {
	q := "EXPLAIN (FORMAT JSON, ANALYZE, BUFFERS) " + sql
	row := db.QueryRowContext(ctx, q, args...)
	var raw []byte
	if err := row.Scan(&raw); err != nil {
		return nil, fmt.Errorf("scan EXPLAIN output: %w", err)
	}
	// Postgres returns a JSON array with a single element; that
	// element has a top-level "Plan" key whose value is the root
	// plan node.
	var arr []map[string]any
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil, fmt.Errorf("unmarshal EXPLAIN JSON: %w", err)
	}
	if len(arr) == 0 {
		return nil, fmt.Errorf("EXPLAIN returned empty array")
	}
	plan, ok := arr[0]["Plan"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("EXPLAIN payload missing top-level Plan key")
	}
	return plan, nil
}

// planNodes walks the plan tree depth-first, yielding every node
// (root first, children in array order). Postgres represents children
// under the "Plans" key as a []any of map[string]any.
func planNodes(root map[string]any) []map[string]any {
	var out []map[string]any
	var walk func(node map[string]any)
	walk = func(node map[string]any) {
		out = append(out, node)
		children, _ := node["Plans"].([]any)
		for _, c := range children {
			if cm, ok := c.(map[string]any); ok {
				walk(cm)
			}
		}
	}
	walk(root)
	return out
}

// summarisePlan returns a short, human-readable shape of the plan tree
// for failure messages. The format is "NodeType[Relation:Index]" per
// node, joined by " -> ". Detailed enough to spot a Seq Scan that
// should have been an Index Scan, terse enough to fit in a test log.
func summarisePlan(root map[string]any) string {
	var parts []string
	for _, n := range planNodes(root) {
		nt, _ := n["Node Type"].(string)
		rel, _ := n["Relation Name"].(string)
		idx, _ := n["Index Name"].(string)
		seg := nt
		if rel != "" {
			seg += "[" + rel
			if idx != "" {
				seg += ":" + idx
			}
			seg += "]"
		}
		parts = append(parts, seg)
	}
	return strings.Join(parts, " -> ")
}

// ─── Assertion helpers ─────────────────────────────────────────────────
//
// Each helper returns a planAssertion that, on failure, names the table
// and the operation seen — never just "plan mismatch". A future migration
// that legitimately changes the plan should be able to read the failure
// and decide whether to update the expectation or back out the change.

// mustUseIndex asserts that some node in the plan is an Index Scan or
// Index Only Scan whose Index Name equals the supplied name. Use when
// you know the exact index that should drive the query.
func mustUseIndex(indexName string) planAssertion {
	return planAssertion{
		desc: "must use index " + indexName,
		check: func(t *testing.T, name string, plan map[string]any) {
			t.Helper()
			for _, n := range planNodes(plan) {
				nt, _ := n["Node Type"].(string)
				if nt != "Index Scan" && nt != "Index Only Scan" && nt != "Bitmap Index Scan" {
					continue
				}
				if got, _ := n["Index Name"].(string); got == indexName {
					return
				}
			}
			t.Errorf("query %s: expected to use index %q; plan was: %s",
				name, indexName, summarisePlan(plan))
		},
	}
}

// mustNotSeqScan asserts that no node in the plan is a Seq Scan against
// the supplied table. Use when a query is supposed to be index-driven
// regardless of which specific index gets picked.
func mustNotSeqScan(table string) planAssertion {
	return planAssertion{
		desc: "must not Seq Scan " + table,
		check: func(t *testing.T, name string, plan map[string]any) {
			t.Helper()
			for _, n := range planNodes(plan) {
				nt, _ := n["Node Type"].(string)
				rel, _ := n["Relation Name"].(string)
				if nt == "Seq Scan" && rel == table {
					t.Errorf("query %s: unexpected Seq Scan on %q; plan was: %s",
						name, table, summarisePlan(plan))
					return
				}
			}
		},
	}
}

// costCeiling asserts the planner-reported total cost of the root plan
// node is at most `max`. Numbers are guideline-level — Postgres cost
// units are not directly comparable across machines or releases, but a
// 10x jump on a tiny seeded table almost always reflects a real
// regression (lost index, accidental cross join). Failure names the
// table set and the observed cost.
func costCeiling(maxCost float64) planAssertion {
	return planAssertion{
		desc: fmt.Sprintf("total cost <= %.2f", maxCost),
		check: func(t *testing.T, name string, plan map[string]any) {
			t.Helper()
			cost, ok := plan["Total Cost"].(float64)
			if !ok {
				t.Fatalf("query %s: plan root has no numeric Total Cost; raw plan: %v",
					name, plan)
			}
			if cost > maxCost {
				t.Errorf("query %s: total cost %.2f exceeds ceiling %.2f; plan was: %s",
					name, cost, maxCost, summarisePlan(plan))
			}
		},
	}
}
