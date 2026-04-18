// Package postgres bulk insert helpers (P3-PERF-01a).
//
// These methods implement the Put*Bulk / Append*Bulk additions on the Store
// interface. Each one builds a chunked multi-row `INSERT ... VALUES (...),(...)
// ON CONFLICT ...` statement so the control-plane batch writer can flush a
// full buffer in a single round-trip instead of N individual INSERTs.
//
// pgx.CopyFrom is not used here because the project talks to Postgres through
// database/sql + pgx/v5/stdlib, not through pgxpool, and CopyFrom requires
// a native pgx connection. Multi-row INSERT is the next-best option and still
// delivers an order-of-magnitude speedup over per-row Exec.
//
// Chunking: Postgres allows up to 65535 bind parameters per query. We chunk at
// 250 rows — the widest row (server_load, 27 columns) uses 250 * 27 = 6750
// params, well under the 65535 cap. 250 was picked after the P3-PERF-01b
// chunk-size sweep: per-row throughput peaks around 100-250 rows and regresses
// at 500+ because the generated SQL and argument slice both grow
// super-linearly with chunk size. See docs/benchmarks/phase3-bulk-insert.md
// for the raw ns/row numbers. Every bulk method runs inside a single
// transaction so partial failure rolls the whole batch back.
package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// bulkChunkSize caps how many rows go into a single multi-row INSERT. See the
// package doc for why 250 is safe across every bulk method (P3-PERF-01b
// tuning: 500 -> 250 after benchmark sweep).
const bulkChunkSize = 250

// placeholders returns the VALUES ($n...) list for `rows` rows of `cols`
// columns each, starting parameter numbering at 1. Kept as a helper so each
// bulk method can build its statement without hand-rolling parameter indices.
func placeholders(rows, cols int) string {
	var b strings.Builder
	// rough over-allocation: up to 10 chars per placeholder
	b.Grow(rows * cols * 10)
	for r := 0; r < rows; r++ {
		if r > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('(')
		for c := 0; c < cols; c++ {
			if c > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, "$%d", r*cols+c+1)
		}
		b.WriteByte(')')
	}
	return b.String()
}

// execInTx runs fn inside a new transaction bound to the top-level *sql.DB,
// committing on nil error and rolling back otherwise. Bulk methods use this so
// a mid-flush failure rolls every chunk back, preserving all-or-nothing
// semantics for the caller. When the Store is already tx-bound (Transact
// scope), the caller's executor is reused and fn runs without opening a new
// transaction — that mirrors how single-row methods already compose inside
// Transact.
func (s *Store) execInTx(ctx context.Context, fn func(exec dbExecutor) error) error {
	if s.sqlDB == nil {
		// Inside Transact — reuse the caller's executor so the bulk writes
		// land in the outer transaction.
		return fn(s.db)
	}
	tx, err := s.sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if err := fn(txExecutor{tx: tx}); err != nil {
		return err
	}
	return tx.Commit()
}

// txExecutor adapts *sql.Tx to the dbExecutor surface used by bulk methods.
type txExecutor struct {
	tx *sql.Tx
}

func (t txExecutor) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return t.tx.ExecContext(ctx, query, args...)
}

func (t txExecutor) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return t.tx.QueryContext(ctx, query, args...)
}

func (t txExecutor) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return t.tx.QueryRowContext(ctx, query, args...)
}

// PutAgentsBulk upserts a batch of agents in a single transaction using
// chunked multi-row INSERT. See Store.PutAgentsBulk in storage/store.go for
// the full contract.
func (s *Store) PutAgentsBulk(ctx context.Context, agents []storage.AgentRecord) error {
	if len(agents) == 0 {
		return nil
	}
	const cols = 8
	return s.execInTx(ctx, func(exec dbExecutor) error {
		for start := 0; start < len(agents); start += bulkChunkSize {
			end := start + bulkChunkSize
			if end > len(agents) {
				end = len(agents)
			}
			chunk := agents[start:end]
			args := make([]any, 0, len(chunk)*cols)
			for _, agent := range chunk {
				var fleetGroupID sql.NullString
				if agent.FleetGroupID != "" {
					fleetGroupID.Valid = true
					fleetGroupID.String = agent.FleetGroupID
				}
				var certIssuedAt sql.NullTime
				if agent.CertIssuedAt != nil {
					certIssuedAt.Valid = true
					certIssuedAt.Time = agent.CertIssuedAt.UTC()
				}
				var certExpiresAt sql.NullTime
				if agent.CertExpiresAt != nil {
					certExpiresAt.Valid = true
					certExpiresAt.Time = agent.CertExpiresAt.UTC()
				}
				args = append(args,
					agent.ID, agent.NodeName, fleetGroupID, agent.Version,
					agent.ReadOnly, agent.LastSeenAt.UTC(), certIssuedAt, certExpiresAt,
				)
			}
			query := `INSERT INTO agents (id, node_name, fleet_group_id, version, read_only, last_seen_at, cert_issued_at, cert_expires_at) VALUES ` +
				placeholders(len(chunk), cols) +
				` ON CONFLICT (id) DO UPDATE SET
					node_name = EXCLUDED.node_name,
					fleet_group_id = EXCLUDED.fleet_group_id,
					version = EXCLUDED.version,
					read_only = EXCLUDED.read_only,
					last_seen_at = EXCLUDED.last_seen_at,
					cert_issued_at = EXCLUDED.cert_issued_at,
					cert_expires_at = EXCLUDED.cert_expires_at`
			if _, err := exec.ExecContext(ctx, query, args...); err != nil {
				return err
			}
		}
		return nil
	})
}

// PutInstancesBulk upserts a batch of Telemt instances. See Store.PutInstancesBulk.
func (s *Store) PutInstancesBulk(ctx context.Context, instances []storage.InstanceRecord) error {
	if len(instances) == 0 {
		return nil
	}
	const cols = 8
	return s.execInTx(ctx, func(exec dbExecutor) error {
		for start := 0; start < len(instances); start += bulkChunkSize {
			end := start + bulkChunkSize
			if end > len(instances) {
				end = len(instances)
			}
			chunk := instances[start:end]
			args := make([]any, 0, len(chunk)*cols)
			for _, instance := range chunk {
				args = append(args,
					instance.ID, instance.AgentID, instance.Name, instance.Version,
					instance.ConfigFingerprint, instance.ConnectedUsers, instance.ReadOnly,
					instance.UpdatedAt.UTC(),
				)
			}
			query := `INSERT INTO telemt_instances (id, agent_id, name, version, config_fingerprint, connected_users, read_only, updated_at) VALUES ` +
				placeholders(len(chunk), cols) +
				` ON CONFLICT (id) DO UPDATE SET
					agent_id = EXCLUDED.agent_id,
					name = EXCLUDED.name,
					version = EXCLUDED.version,
					config_fingerprint = EXCLUDED.config_fingerprint,
					connected_users = EXCLUDED.connected_users,
					read_only = EXCLUDED.read_only,
					updated_at = EXCLUDED.updated_at`
			if _, err := exec.ExecContext(ctx, query, args...); err != nil {
				return err
			}
		}
		return nil
	})
}

// AppendMetricSnapshotsBulk inserts a batch of metric snapshots. Rows have a
// synthetic ID primary key so no ON CONFLICT clause is needed — same as the
// single-row AppendMetricSnapshot.
func (s *Store) AppendMetricSnapshotsBulk(ctx context.Context, snapshots []storage.MetricSnapshotRecord) error {
	if len(snapshots) == 0 {
		return nil
	}
	const cols = 5
	return s.execInTx(ctx, func(exec dbExecutor) error {
		for start := 0; start < len(snapshots); start += bulkChunkSize {
			end := start + bulkChunkSize
			if end > len(snapshots) {
				end = len(snapshots)
			}
			chunk := snapshots[start:end]
			args := make([]any, 0, len(chunk)*cols)
			for _, snapshot := range chunk {
				valuesJSON, err := encodeJSON(snapshot.Values)
				if err != nil {
					return err
				}
				args = append(args,
					snapshot.ID, snapshot.AgentID, snapshot.InstanceID,
					snapshot.CapturedAt.UTC(), valuesJSON,
				)
			}
			query := `INSERT INTO metric_snapshots (id, agent_id, instance_id, captured_at, values) VALUES ` +
				placeholders(len(chunk), cols)
			if _, err := exec.ExecContext(ctx, query, args...); err != nil {
				return err
			}
		}
		return nil
	})
}

// AppendServerLoadPointsBulk inserts a batch of server-load points. Matches
// the single-row INSERT ... ON CONFLICT (agent_id, captured_at) DO NOTHING
// semantics so duplicate (agent,capture) pairs do not error.
func (s *Store) AppendServerLoadPointsBulk(ctx context.Context, records []storage.ServerLoadPointRecord) error {
	if len(records) == 0 {
		return nil
	}
	const cols = 27
	return s.execInTx(ctx, func(exec dbExecutor) error {
		for start := 0; start < len(records); start += bulkChunkSize {
			end := start + bulkChunkSize
			if end > len(records) {
				end = len(records)
			}
			chunk := records[start:end]
			args := make([]any, 0, len(chunk)*cols)
			for _, r := range chunk {
				args = append(args,
					r.AgentID, r.CapturedAt.UTC(),
					r.CPUPctAvg, r.CPUPctMax, r.MemPctAvg, r.MemPctMax,
					r.DiskPctAvg, r.DiskPctMax, r.Load1M, r.Load5M, r.Load15M,
					r.ConnectionsAvg, r.ConnectionsMax, r.ConnectionsMEAvg, r.ConnectionsDirectAvg,
					r.ActiveUsersAvg, r.ActiveUsersMax,
					r.ConnectionsTotal, r.ConnectionsBadTotal, r.HandshakeTimeoutsTotal,
					r.DCCoverageMinPct, r.DCCoverageAvgPct,
					r.HealthyUpstreams, r.TotalUpstreams, r.NetBytesSent, r.NetBytesRecv, r.SampleCount,
				)
			}
			query := `INSERT INTO ts_server_load (
					agent_id, captured_at,
					cpu_pct_avg, cpu_pct_max, mem_pct_avg, mem_pct_max,
					disk_pct_avg, disk_pct_max, load_1m, load_5m, load_15m,
					connections_avg, connections_max, connections_me_avg, connections_direct_avg,
					active_users_avg, active_users_max,
					connections_total, connections_bad_total, handshake_timeouts_total,
					dc_coverage_min_pct, dc_coverage_avg_pct,
					healthy_upstreams, total_upstreams, net_bytes_sent, net_bytes_recv, sample_count
				) VALUES ` + placeholders(len(chunk), cols) +
				` ON CONFLICT (agent_id, captured_at) DO NOTHING`
			if _, err := exec.ExecContext(ctx, query, args...); err != nil {
				return err
			}
		}
		return nil
	})
}

// AppendDCHealthPointsBulk inserts a batch of DC-health points. Same ON
// CONFLICT DO NOTHING semantics as the single-row variant.
func (s *Store) AppendDCHealthPointsBulk(ctx context.Context, records []storage.DCHealthPointRecord) error {
	if len(records) == 0 {
		return nil
	}
	const cols = 11
	return s.execInTx(ctx, func(exec dbExecutor) error {
		for start := 0; start < len(records); start += bulkChunkSize {
			end := start + bulkChunkSize
			if end > len(records) {
				end = len(records)
			}
			chunk := records[start:end]
			args := make([]any, 0, len(chunk)*cols)
			for _, r := range chunk {
				args = append(args,
					r.AgentID, r.CapturedAt.UTC(), r.DC,
					r.CoveragePctAvg, r.CoveragePctMin, r.RTTMsAvg, r.RTTMsMax,
					r.AliveWritersMin, r.RequiredWriters, r.LoadMax, r.SampleCount,
				)
			}
			query := `INSERT INTO ts_dc_health (
					agent_id, captured_at, dc,
					coverage_pct_avg, coverage_pct_min, rtt_ms_avg, rtt_ms_max,
					alive_writers_min, required_writers, load_max, sample_count
				) VALUES ` + placeholders(len(chunk), cols) +
				` ON CONFLICT (agent_id, dc, captured_at) DO NOTHING`
			if _, err := exec.ExecContext(ctx, query, args...); err != nil {
				return err
			}
		}
		return nil
	})
}

// UpsertClientIPHistoryBulk upserts a batch of client-ip history rows. Same
// ON CONFLICT (agent_id, client_id, ip_address) DO UPDATE SET last_seen as
// the single-row variant; when the same (agent, client, ip) key appears twice
// in one batch, the last row's last_seen wins.
func (s *Store) UpsertClientIPHistoryBulk(ctx context.Context, records []storage.ClientIPHistoryRecord) error {
	if len(records) == 0 {
		return nil
	}
	const cols = 5
	return s.execInTx(ctx, func(exec dbExecutor) error {
		for start := 0; start < len(records); start += bulkChunkSize {
			end := start + bulkChunkSize
			if end > len(records) {
				end = len(records)
			}
			chunk := records[start:end]
			args := make([]any, 0, len(chunk)*cols)
			for _, r := range chunk {
				args = append(args,
					r.AgentID, r.ClientID, r.IPAddress,
					r.FirstSeen.UTC(), r.LastSeen.UTC(),
				)
			}
			query := `INSERT INTO client_ip_history (agent_id, client_id, ip_address, first_seen, last_seen) VALUES ` +
				placeholders(len(chunk), cols) +
				` ON CONFLICT (agent_id, client_id, ip_address) DO UPDATE
					SET last_seen = EXCLUDED.last_seen`
			if _, err := exec.ExecContext(ctx, query, args...); err != nil {
				return err
			}
		}
		return nil
	})
}
