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
// super-linearly with chunk size. See docs/benchmarks/phase3-bulk-insert.md
// for the raw ns/row numbers.
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
	defer func() {
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
				var certIssuedAtUnix sql.NullInt64
				if agent.CertIssuedAt != nil {
					certIssuedAtUnix.Valid = true
					certIssuedAtUnix.Int64 = agent.CertIssuedAt.UTC().Unix()
				}
				var certExpiresAtUnix sql.NullInt64
				if agent.CertExpiresAt != nil {
					certExpiresAtUnix.Valid = true
					certExpiresAtUnix.Int64 = agent.CertExpiresAt.UTC().Unix()
				}
				args = append(args,
					agent.ID, agent.NodeName, fleetGroupID, agent.Version,
					boolToInt(agent.ReadOnly), toUnix(agent.LastSeenAt),
					certIssuedAtUnix, certExpiresAtUnix,
				)
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
					toUnix(snapshot.CapturedAt), valuesJSON,
				)
			}
			query := fmt.Sprintf(
				`INSERT INTO metric_snapshots (id, agent_id, instance_id, captured_at_unix, "values") VALUES %s`,
				rowPlaceholders(len(chunk), cols))
			if _, err := exec.ExecContext(ctx, query, args...); err != nil {
				return err
			}
		}
		return nil
	})
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
		for start := 0; start < len(records); start += bulkChunkSize {
			end := start + bulkChunkSize
			if end > len(records) {
				end = len(records)
			}
			chunk := records[start:end]
			args := make([]any, 0, len(chunk)*cols)
			for _, r := range chunk {
				args = append(args,
					r.AgentID, toUnix(r.CapturedAt),
					r.CPUPctAvg, r.CPUPctMax, r.MemPctAvg, r.MemPctMax,
					r.DiskPctAvg, r.DiskPctMax, r.Load1M, r.Load5M, r.Load15M,
					r.ConnectionsAvg, r.ConnectionsMax, r.ConnectionsMEAvg, r.ConnectionsDirectAvg,
					r.ActiveUsersAvg, r.ActiveUsersMax,
					r.ConnectionsTotal, r.ConnectionsBadTotal, r.HandshakeTimeoutsTotal,
					r.DCCoverageMinPct, r.DCCoverageAvgPct,
					r.HealthyUpstreams, r.TotalUpstreams, r.NetBytesSent, r.NetBytesRecv, r.SampleCount,
				)
			}
			query := fmt.Sprintf(
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
				rowPlaceholders(len(chunk), cols))
			if _, err := exec.ExecContext(ctx, query, args...); err != nil {
				return err
			}
		}
		return nil
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
