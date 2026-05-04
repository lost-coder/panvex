// Package sqlite bulk insert helpers (P3-PERF-01a).
//
// SQLite has a hard cap on bound parameters per statement (default 32766
// as of SQLite 3.32; older builds and modernc defaults sit at 999). We chunk
// at 250 rows: the widest method here (ts_server_load, 27 columns) uses
// 250 * 27 = 6750 parameters — comfortably under both the modernc default
// (999 would already be exceeded at 37+ rows of server_load, so the real
// ceiling comes from SQLITE_MAX_VARIABLE_NUMBER = 32766) and the 65535 bind
// cap on the Postgres side. 250 was picked after the P3-PERF-01b chunk-size
// sweep: per-row throughput peaks around 100-250 rows on SQLite and regresses
// noticeably at 500+ because the generated SQL and argument slice both grow
// super-linearly with chunk size.
//
// Every bulk call runs inside a single transaction so partial failure rolls
// the whole batch back — same atomicity guarantee callers get from
// Store.Transact.
package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// bulkChunkSize mirrors the Postgres helper — see package doc for rationale
// (P3-PERF-01b tuning: 500 -> 250 after benchmark sweep).
const bulkChunkSize = 250

// rowPlaceholders returns "(?,?,...)," groups repeated `rows` times, each with
// `cols` placeholders. Kept separate from Postgres's numbered $N version
// because SQLite uses `?` only.
func rowPlaceholders(rows, cols int) string {
	// Build a single "(?,?,?)" group once, then join copies with commas.
	var group strings.Builder
	group.Grow(2 + cols*2)
	group.WriteByte('(')
	for c := 0; c < cols; c++ {
		if c > 0 {
			group.WriteByte(',')
		}
		group.WriteByte('?')
	}
	group.WriteByte(')')
	g := group.String()

	var b strings.Builder
	b.Grow(rows * (len(g) + 1))
	for r := 0; r < rows; r++ {
		if r > 0 {
			b.WriteByte(',')
		}
		b.WriteString(g)
	}
	return b.String()
}

// execInTx runs fn inside a transaction obtained from s.sqlDB, or reuses the
// caller's executor when the Store is already bound inside Transact. SQLite is
// single-writer, so BEGIN IMMEDIATE is used to grab the writer lock up-front —
// matching Transact's behaviour in store.go.
func (s *Store) execInTx(ctx context.Context, fn func(exec dbExecutor) error) error {
	if s.sqlDB == nil {
		return fn(s.db)
	}
	conn, err := s.sqlDB.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}
	committed := false
	// ROLLBACK runs in defer and must complete even when the caller's ctx
	// has already been canceled — otherwise we'd leave the writer lock
	// held. context.Background() is intentional here.
	defer func() { //nolint:contextcheck // deferred cleanup must outlive caller ctx
		if !committed {
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		}
	}()

	if err := fn(connExecutor{conn: conn}); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return err
	}
	committed = true
	return nil
}

// PutAgentsBulk upserts a batch of agents. Same per-row semantics as PutAgent
// (UPSERT on id); duplicate IDs in the same batch collapse to last-wins.
// nullStringFrom returns a sql.NullString that is Valid only when the
// raw string is non-empty.
func nullStringFrom(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// chunkBounds returns [start,end) for the next chunk that fits within total.
func chunkBounds(start, total int) (int, int) {
	end := start + bulkChunkSize
	if end > total {
		end = total
	}
	return start, end
}

// runBulkChunks runs fn over fixed-size slices of length-`total`, executing one
// SQL statement per chunk via `exec`. The query is rebuilt per chunk because
// the placeholder count varies for the trailing chunk.
//
// queryFn formats the final SQL given a placeholder block; argsFn appends the
// flattened parameter slice for one chunk. This is the shared scaffolding all
// PutXxxBulk / AppendXxxBulk methods used to repeat inline.
func runBulkChunks(
	ctx context.Context,
	exec dbExecutor,
	total, cols int,
	queryFn func(placeholders string) string,
	argsFn func(start, end int) ([]any, error),
) error {
	for start := 0; start < total; start += bulkChunkSize {
		s, e := chunkBounds(start, total)
		args, err := argsFn(s, e)
		if err != nil {
			return err
		}
		query := queryFn(rowPlaceholders(e-s, cols))
		if _, err := exec.ExecContext(ctx, query, args...); err != nil {
			return err
		}
	}
	return nil
}

// nullUnixFromPtr returns a sql.NullInt64 carrying the UTC Unix
// seconds for `t`, or an invalid value when `t` is nil.
func nullUnixFromPtr(t *time.Time) sql.NullInt64 {
	if t == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: t.UTC().Unix(), Valid: true}
}

// agentBulkArgs flattens an AgentRecord into the parameter slice used
// by PutAgentsBulk. Splitting it out keeps the per-chunk loop body
// simple.
func agentBulkArgs(agent storage.AgentRecord) []any {
	return []any{
		agent.ID, agent.NodeName, nullStringFrom(agent.FleetGroupID), agent.Version,
		boolToInt(agent.ReadOnly), toUnix(agent.LastSeenAt),
		nullUnixFromPtr(agent.CertIssuedAt), nullUnixFromPtr(agent.CertExpiresAt),
	}
}

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
				args = append(args, agentBulkArgs(agent)...)
			}
			query := fmt.Sprintf(
				`INSERT INTO agents (id, node_name, fleet_group_id, version, read_only, last_seen_at_unix, cert_issued_at_unix, cert_expires_at_unix) VALUES %s
				ON CONFLICT(id) DO UPDATE SET
					node_name = excluded.node_name,
					fleet_group_id = excluded.fleet_group_id,
					version = excluded.version,
					read_only = excluded.read_only,
					last_seen_at_unix = excluded.last_seen_at_unix,
					cert_issued_at_unix = excluded.cert_issued_at_unix,
					cert_expires_at_unix = excluded.cert_expires_at_unix`,
				rowPlaceholders(len(chunk), cols))
			if _, err := exec.ExecContext(ctx, query, args...); err != nil {
				return err
			}
		}
		return nil
	})
}

// PutInstancesBulk upserts a batch of Telemt instances.
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
					instance.ConfigFingerprint, instance.ConnectedUsers,
					boolToInt(instance.ReadOnly), toUnix(instance.UpdatedAt),
				)
			}
			query := fmt.Sprintf(
				`INSERT INTO telemt_instances (id, agent_id, name, version, config_fingerprint, connected_users, read_only, updated_at_unix) VALUES %s
				ON CONFLICT(id) DO UPDATE SET
					agent_id = excluded.agent_id,
					name = excluded.name,
					version = excluded.version,
					config_fingerprint = excluded.config_fingerprint,
					connected_users = excluded.connected_users,
					read_only = excluded.read_only,
					updated_at_unix = excluded.updated_at_unix`,
				rowPlaceholders(len(chunk), cols))
			if _, err := exec.ExecContext(ctx, query, args...); err != nil {
				return err
			}
		}
		return nil
	})
}

// AppendMetricSnapshotsBulk inserts a batch of metric snapshots. The `values`
// column is a reserved SQLite keyword so it must be double-quoted — same as
// the single-row method.
func (s *Store) AppendMetricSnapshotsBulk(ctx context.Context, snapshots []storage.MetricSnapshotRecord) error {
	if len(snapshots) == 0 {
		return nil
	}
	const cols = 5
	return s.execInTx(ctx, func(exec dbExecutor) error {
		return runBulkChunks(ctx, exec, len(snapshots), cols,
			func(placeholders string) string {
				return fmt.Sprintf(
					`INSERT INTO metric_snapshots (id, agent_id, instance_id, captured_at_unix, "values") VALUES %s`,
					placeholders)
			},
			func(start, end int) ([]any, error) {
				return metricSnapshotChunkArgs(snapshots[start:end], cols)
			},
		)
	})
}

func metricSnapshotChunkArgs(chunk []storage.MetricSnapshotRecord, cols int) ([]any, error) {
	args := make([]any, 0, len(chunk)*cols)
	for _, snapshot := range chunk {
		valuesJSON, err := encodeJSON(snapshot.Values)
		if err != nil {
			return nil, err
		}
		args = append(args,
			snapshot.ID, snapshot.AgentID, snapshot.InstanceID,
			toUnix(snapshot.CapturedAt), valuesJSON,
		)
	}
	return args, nil
}

// serverLoadBulkArgs flattens one ServerLoadPointRecord into the bulk arg
// slice for AppendServerLoadPointsBulk. Splitting it keeps the chunk loop
// shallow (Sonar S3776).
func serverLoadBulkArgs(r storage.ServerLoadPointRecord) []any {
	return []any{
		r.AgentID, toUnix(r.CapturedAt),
		r.CPUPctAvg, r.CPUPctMax, r.MemPctAvg, r.MemPctMax,
		r.DiskPctAvg, r.DiskPctMax, r.Load1M, r.Load5M, r.Load15M,
		r.ConnectionsAvg, r.ConnectionsMax, r.ConnectionsMEAvg, r.ConnectionsDirectAvg,
		r.ActiveUsersAvg, r.ActiveUsersMax,
		r.ConnectionsTotal, r.ConnectionsBadTotal, r.HandshakeTimeoutsTotal,
		r.DCCoverageMinPct, r.DCCoverageAvgPct,
		r.HealthyUpstreams, r.TotalUpstreams, r.NetBytesSent, r.NetBytesRecv, r.SampleCount,
	}
}

// AppendServerLoadPointsBulk inserts a batch of server-load points. Uses
// `INSERT OR IGNORE` to match the single-row method's semantics — duplicate
// (agent_id, captured_at_unix) rows are silently skipped.
func (s *Store) AppendServerLoadPointsBulk(ctx context.Context, records []storage.ServerLoadPointRecord) error {
	if len(records) == 0 {
		return nil
	}
	const cols = 27
	return s.execInTx(ctx, func(exec dbExecutor) error {
		return runBulkChunks(ctx, exec, len(records), cols,
			func(placeholders string) string {
				return fmt.Sprintf(
					`INSERT OR IGNORE INTO ts_server_load (
						agent_id, captured_at_unix,
						cpu_pct_avg, cpu_pct_max, mem_pct_avg, mem_pct_max,
						disk_pct_avg, disk_pct_max, load_1m, load_5m, load_15m,
						connections_avg, connections_max, connections_me_avg, connections_direct_avg,
						active_users_avg, active_users_max,
						connections_total, connections_bad_total, handshake_timeouts_total,
						dc_coverage_min_pct, dc_coverage_avg_pct,
						healthy_upstreams, total_upstreams, net_bytes_sent, net_bytes_recv, sample_count
					) VALUES %s`,
					placeholders)
			},
			func(start, end int) ([]any, error) {
				args := make([]any, 0, (end-start)*cols)
				for _, r := range records[start:end] {
					args = append(args, serverLoadBulkArgs(r)...)
				}
				return args, nil
			},
		)
	})
}

// AppendDCHealthPointsBulk inserts a batch of DC-health points.
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
					r.AgentID, toUnix(r.CapturedAt), r.DC,
					r.CoveragePctAvg, r.CoveragePctMin, r.RTTMsAvg, r.RTTMsMax,
					r.AliveWritersMin, r.RequiredWriters, r.LoadMax, r.SampleCount,
				)
			}
			query := fmt.Sprintf(
				`INSERT OR IGNORE INTO ts_dc_health (
					agent_id, captured_at_unix, dc,
					coverage_pct_avg, coverage_pct_min, rtt_ms_avg, rtt_ms_max,
					alive_writers_min, required_writers, load_max, sample_count
				) VALUES %s`,
				rowPlaceholders(len(chunk), cols))
			if _, err := exec.ExecContext(ctx, query, args...); err != nil {
				return err
			}
		}
		return nil
	})
}

// clientUsageBulkArgs flattens one ClientUsageRecord into the parameter slice
// used by UpsertClientUsageBulk. Splitting it out keeps the per-chunk loop
// body simple (Sonar S3776).
func clientUsageBulkArgs(r storage.ClientUsageRecord) []any {
	return []any{
		r.ClientID, r.AgentID,
		int64(r.TrafficUsedBytes), r.UniqueIPsUsed,
		r.ActiveTCPConns, r.ActiveUniqueIPs,
		int64(r.LastSeq), toUnix(r.ObservedAt),
	}
}

// UpsertClientUsageBulk upserts a batch of (client, agent) usage counters in
// a single transaction. Same ON CONFLICT (client_id, agent_id) DO UPDATE
// semantics as the single-row UpsertClientUsage; duplicate keys within one
// batch collapse to last-write-wins. P-1 (sprint S-23 perf-critical) — the
// hot-path agent-flow tick was issuing N single-row Exec calls per snapshot
// (500 clients x 50 agents = 25k round-trips); this batches them into one.
func (s *Store) UpsertClientUsageBulk(ctx context.Context, records []storage.ClientUsageRecord) error {
	if len(records) == 0 {
		return nil
	}
	const cols = 8
	return s.execInTx(ctx, func(exec dbExecutor) error {
		return runBulkChunks(ctx, exec, len(records), cols,
			func(placeholders string) string {
				return fmt.Sprintf(
					`INSERT INTO client_usage (
						client_id, agent_id, traffic_used_bytes, unique_ips_used,
						active_tcp_conns, active_unique_ips, last_seq, observed_at_unix
					) VALUES %s
					ON CONFLICT(client_id, agent_id) DO UPDATE SET
						traffic_used_bytes = excluded.traffic_used_bytes,
						unique_ips_used    = excluded.unique_ips_used,
						active_tcp_conns   = excluded.active_tcp_conns,
						active_unique_ips  = excluded.active_unique_ips,
						last_seq           = excluded.last_seq,
						observed_at_unix   = excluded.observed_at_unix`,
					placeholders)
			},
			func(start, end int) ([]any, error) {
				args := make([]any, 0, (end-start)*cols)
				for _, r := range records[start:end] {
					args = append(args, clientUsageBulkArgs(r)...)
				}
				return args, nil
			},
		)
	})
}

// UpsertClientIPHistoryBulk upserts a batch of client-ip history rows. Same
// ON CONFLICT (agent_id, client_id, ip_address) DO UPDATE SET last_seen_unix
// as the single-row variant.
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
					toUnix(r.FirstSeen), toUnix(r.LastSeen),
				)
			}
			query := fmt.Sprintf(
				`INSERT INTO client_ip_history (agent_id, client_id, ip_address, first_seen_unix, last_seen_unix) VALUES %s
				ON CONFLICT (agent_id, client_id, ip_address) DO UPDATE
					SET last_seen_unix = excluded.last_seen_unix`,
				rowPlaceholders(len(chunk), cols))
			if _, err := exec.ExecContext(ctx, query, args...); err != nil {
				return err
			}
		}
		return nil
	})
}
