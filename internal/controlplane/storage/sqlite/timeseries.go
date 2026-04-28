package sqlite

import (
	"context"
	"strings"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

func (s *Store) AppendServerLoadPoint(ctx context.Context, record storage.ServerLoadPointRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO ts_server_load (
			agent_id, captured_at_unix,
			cpu_pct_avg, cpu_pct_max, mem_pct_avg, mem_pct_max,
			disk_pct_avg, disk_pct_max, load_1m, load_5m, load_15m,
			connections_avg, connections_max, connections_me_avg, connections_direct_avg,
			active_users_avg, active_users_max,
			connections_total, connections_bad_total, handshake_timeouts_total,
			dc_coverage_min_pct, dc_coverage_avg_pct,
			healthy_upstreams, total_upstreams, net_bytes_sent, net_bytes_recv, sample_count
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
	`,
		record.AgentID, toUnix(record.CapturedAt),
		record.CPUPctAvg, record.CPUPctMax, record.MemPctAvg, record.MemPctMax,
		record.DiskPctAvg, record.DiskPctMax, record.Load1M, record.Load5M, record.Load15M,
		record.ConnectionsAvg, record.ConnectionsMax, record.ConnectionsMEAvg, record.ConnectionsDirectAvg,
		record.ActiveUsersAvg, record.ActiveUsersMax,
		record.ConnectionsTotal, record.ConnectionsBadTotal, record.HandshakeTimeoutsTotal,
		record.DCCoverageMinPct, record.DCCoverageAvgPct,
		record.HealthyUpstreams, record.TotalUpstreams, record.NetBytesSent, record.NetBytesRecv, record.SampleCount,
	)
	return err
}

func (s *Store) ListServerLoadPoints(ctx context.Context, agentID string, from time.Time, to time.Time) ([]storage.ServerLoadPointRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT agent_id, captured_at_unix,
			cpu_pct_avg, cpu_pct_max, mem_pct_avg, mem_pct_max,
			disk_pct_avg, disk_pct_max, load_1m, load_5m, load_15m,
			connections_avg, connections_max, connections_me_avg, connections_direct_avg,
			active_users_avg, active_users_max,
			connections_total, connections_bad_total, handshake_timeouts_total,
			dc_coverage_min_pct, dc_coverage_avg_pct,
			healthy_upstreams, total_upstreams, net_bytes_sent, net_bytes_recv, sample_count
		FROM ts_server_load
		WHERE agent_id = ? AND captured_at_unix >= ? AND captured_at_unix <= ?
		ORDER BY captured_at_unix
	`, agentID, toUnix(from), toUnix(to))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []storage.ServerLoadPointRecord
	for rows.Next() {
		var r storage.ServerLoadPointRecord
		var capturedAtUnix int64
		if err := rows.Scan(
			&r.AgentID, &capturedAtUnix,
			&r.CPUPctAvg, &r.CPUPctMax, &r.MemPctAvg, &r.MemPctMax,
			&r.DiskPctAvg, &r.DiskPctMax, &r.Load1M, &r.Load5M, &r.Load15M,
			&r.ConnectionsAvg, &r.ConnectionsMax, &r.ConnectionsMEAvg, &r.ConnectionsDirectAvg,
			&r.ActiveUsersAvg, &r.ActiveUsersMax,
			&r.ConnectionsTotal, &r.ConnectionsBadTotal, &r.HandshakeTimeoutsTotal,
			&r.DCCoverageMinPct, &r.DCCoverageAvgPct,
			&r.HealthyUpstreams, &r.TotalUpstreams, &r.NetBytesSent, &r.NetBytesRecv, &r.SampleCount,
		); err != nil {
			return nil, err
		}
		r.CapturedAt = fromUnix(capturedAtUnix)
		result = append(result, r)
	}
	return result, rows.Err()
}

func (s *Store) PruneServerLoadPoints(ctx context.Context, olderThan time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM ts_server_load WHERE captured_at_unix < ?`, toUnix(olderThan))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// ListServerLoadPointsForAgents returns load points for a batch of
// agents in a single round-trip (Q2.U-P-01). Each agent's slice is
// sorted by captured_at ascending; missing agents are absent from the
// map.
func (s *Store) ListServerLoadPointsForAgents(ctx context.Context, agentIDs []string, from time.Time, to time.Time) (map[string][]storage.ServerLoadPointRecord, error) {
	out := make(map[string][]storage.ServerLoadPointRecord, len(agentIDs))
	if len(agentIDs) == 0 {
		return out, nil
	}
	placeholders := make([]string, len(agentIDs))
	args := make([]any, 0, len(agentIDs)+2)
	for i, id := range agentIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}
	args = append(args, toUnix(from), toUnix(to))
	query := `
		SELECT agent_id, captured_at_unix,
			cpu_pct_avg, cpu_pct_max, mem_pct_avg, mem_pct_max,
			disk_pct_avg, disk_pct_max, load_1m, load_5m, load_15m,
			connections_avg, connections_max, connections_me_avg, connections_direct_avg,
			active_users_avg, active_users_max,
			connections_total, connections_bad_total, handshake_timeouts_total,
			dc_coverage_min_pct, dc_coverage_avg_pct,
			healthy_upstreams, total_upstreams, net_bytes_sent, net_bytes_recv, sample_count
		FROM ts_server_load
		WHERE agent_id IN (` + strings.Join(placeholders, ",") + `)
		  AND captured_at_unix >= ? AND captured_at_unix <= ?
		ORDER BY agent_id, captured_at_unix
	`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var r storage.ServerLoadPointRecord
		var capturedAtUnix int64
		if err := rows.Scan(
			&r.AgentID, &capturedAtUnix,
			&r.CPUPctAvg, &r.CPUPctMax, &r.MemPctAvg, &r.MemPctMax,
			&r.DiskPctAvg, &r.DiskPctMax, &r.Load1M, &r.Load5M, &r.Load15M,
			&r.ConnectionsAvg, &r.ConnectionsMax, &r.ConnectionsMEAvg, &r.ConnectionsDirectAvg,
			&r.ActiveUsersAvg, &r.ActiveUsersMax,
			&r.ConnectionsTotal, &r.ConnectionsBadTotal, &r.HandshakeTimeoutsTotal,
			&r.DCCoverageMinPct, &r.DCCoverageAvgPct,
			&r.HealthyUpstreams, &r.TotalUpstreams, &r.NetBytesSent, &r.NetBytesRecv, &r.SampleCount,
		); err != nil {
			return nil, err
		}
		r.CapturedAt = fromUnix(capturedAtUnix)
		out[r.AgentID] = append(out[r.AgentID], r)
	}
	return out, rows.Err()
}

func (s *Store) AppendDCHealthPoint(ctx context.Context, record storage.DCHealthPointRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO ts_dc_health (
			agent_id, captured_at_unix, dc,
			coverage_pct_avg, coverage_pct_min, rtt_ms_avg, rtt_ms_max,
			alive_writers_min, required_writers, load_max, sample_count
		) VALUES (?,?,?,?,?,?,?,?,?,?,?)
	`,
		record.AgentID, toUnix(record.CapturedAt), record.DC,
		record.CoveragePctAvg, record.CoveragePctMin, record.RTTMsAvg, record.RTTMsMax,
		record.AliveWritersMin, record.RequiredWriters, record.LoadMax, record.SampleCount,
	)
	return err
}

func (s *Store) ListDCHealthPoints(ctx context.Context, agentID string, from time.Time, to time.Time) ([]storage.DCHealthPointRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT agent_id, captured_at_unix, dc,
			coverage_pct_avg, coverage_pct_min, rtt_ms_avg, rtt_ms_max,
			alive_writers_min, required_writers, load_max, sample_count
		FROM ts_dc_health
		WHERE agent_id = ? AND captured_at_unix >= ? AND captured_at_unix <= ?
		ORDER BY dc, captured_at_unix
	`, agentID, toUnix(from), toUnix(to))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []storage.DCHealthPointRecord
	for rows.Next() {
		var r storage.DCHealthPointRecord
		var capturedAtUnix int64
		if err := rows.Scan(
			&r.AgentID, &capturedAtUnix, &r.DC,
			&r.CoveragePctAvg, &r.CoveragePctMin, &r.RTTMsAvg, &r.RTTMsMax,
			&r.AliveWritersMin, &r.RequiredWriters, &r.LoadMax, &r.SampleCount,
		); err != nil {
			return nil, err
		}
		r.CapturedAt = fromUnix(capturedAtUnix)
		result = append(result, r)
	}
	return result, rows.Err()
}

func (s *Store) PruneDCHealthPoints(ctx context.Context, olderThan time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM ts_dc_health WHERE captured_at_unix < ?`, toUnix(olderThan))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *Store) UpsertClientIPHistory(ctx context.Context, record storage.ClientIPHistoryRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO client_ip_history (agent_id, client_id, ip_address, first_seen_unix, last_seen_unix)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (agent_id, client_id, ip_address) DO UPDATE
		SET last_seen_unix = excluded.last_seen_unix
	`, record.AgentID, record.ClientID, record.IPAddress, toUnix(record.FirstSeen), toUnix(record.LastSeen))
	return err
}

func (s *Store) ListClientIPHistory(ctx context.Context, clientID string, from time.Time, to time.Time) ([]storage.ClientIPHistoryRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT agent_id, client_id, ip_address, first_seen_unix, last_seen_unix
		FROM client_ip_history
		WHERE client_id = ? AND last_seen_unix >= ? AND first_seen_unix <= ?
		ORDER BY last_seen_unix DESC
	`, clientID, toUnix(from), toUnix(to))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []storage.ClientIPHistoryRecord
	for rows.Next() {
		var r storage.ClientIPHistoryRecord
		var firstSeenUnix, lastSeenUnix int64
		if err := rows.Scan(&r.AgentID, &r.ClientID, &r.IPAddress, &firstSeenUnix, &lastSeenUnix); err != nil {
			return nil, err
		}
		r.FirstSeen = fromUnix(firstSeenUnix)
		r.LastSeen = fromUnix(lastSeenUnix)
		result = append(result, r)
	}
	return result, rows.Err()
}

// AggregateClientIPHistory mirrors the Postgres implementation: per-IP
// aggregate computed by SQL with last_seen DESC ordering and an
// optional LIMIT, so a high-cardinality client cannot stream millions
// of raw rows just to be deduplicated client-side.
func (s *Store) AggregateClientIPHistory(ctx context.Context, clientID string, from time.Time, to time.Time, limit int) ([]storage.ClientIPAggregateRecord, error) {
	query := `
		SELECT ip_address, MIN(first_seen_unix) AS first_seen_unix, MAX(last_seen_unix) AS last_seen_unix
		FROM client_ip_history
		WHERE client_id = ? AND last_seen_unix >= ? AND first_seen_unix <= ?
		GROUP BY ip_address
		ORDER BY last_seen_unix DESC
	`
	args := []any{clientID, toUnix(from), toUnix(to)}
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []storage.ClientIPAggregateRecord
	for rows.Next() {
		var r storage.ClientIPAggregateRecord
		var firstSeenUnix, lastSeenUnix int64
		if err := rows.Scan(&r.IPAddress, &firstSeenUnix, &lastSeenUnix); err != nil {
			return nil, err
		}
		r.FirstSeen = fromUnix(firstSeenUnix)
		r.LastSeen = fromUnix(lastSeenUnix)
		result = append(result, r)
	}
	return result, rows.Err()
}

func (s *Store) CountUniqueClientIPs(ctx context.Context, clientID string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT ip_address) FROM client_ip_history WHERE client_id = ?`, clientID).Scan(&count)
	return count, err
}

// CountUniqueClientIPsForClients computes the unique-IP count for each
// client ID in one query so the /api/clients listing avoids the N+1
// pattern (Q2.U-P-03).
func (s *Store) CountUniqueClientIPsForClients(ctx context.Context, clientIDs []string) (map[string]int, error) {
	out := make(map[string]int, len(clientIDs))
	if len(clientIDs) == 0 {
		return out, nil
	}
	placeholders := make([]string, len(clientIDs))
	args := make([]any, len(clientIDs))
	for i, id := range clientIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	query := `
		SELECT client_id, COUNT(DISTINCT ip_address)
		FROM client_ip_history
		WHERE client_id IN (` + strings.Join(placeholders, ",") + `)
		GROUP BY client_id
	`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var clientID string
		var count int
		if err := rows.Scan(&clientID, &count); err != nil {
			return nil, err
		}
		out[clientID] = count
	}
	return out, rows.Err()
}

func (s *Store) PruneClientIPHistory(ctx context.Context, olderThan time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM client_ip_history WHERE last_seen_unix < ?`, toUnix(olderThan))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *Store) RollupServerLoadHourly(ctx context.Context, bucketHour time.Time) error {
	bucketStart := toUnix(bucketHour.Truncate(time.Hour))
	bucketEnd := bucketStart + 3600
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO ts_server_load_hourly (
			agent_id, bucket_hour_unix,
			cpu_pct_avg, cpu_pct_max, mem_pct_avg, mem_pct_max,
			connections_avg, connections_max, active_users_avg, active_users_max,
			dc_coverage_min, dc_coverage_avg, sample_count
		)
		SELECT agent_id, ?,
			AVG(cpu_pct_avg), MAX(cpu_pct_max), AVG(mem_pct_avg), MAX(mem_pct_max),
			AVG(connections_avg), MAX(connections_max), AVG(active_users_avg), MAX(active_users_max),
			MIN(dc_coverage_min_pct), AVG(dc_coverage_avg_pct), COUNT(*)
		FROM ts_server_load
		WHERE captured_at_unix >= ? AND captured_at_unix < ?
		GROUP BY agent_id
		ON CONFLICT (agent_id, bucket_hour_unix) DO UPDATE SET
			cpu_pct_avg = EXCLUDED.cpu_pct_avg,
			cpu_pct_max = EXCLUDED.cpu_pct_max,
			mem_pct_avg = EXCLUDED.mem_pct_avg,
			mem_pct_max = EXCLUDED.mem_pct_max,
			connections_avg = EXCLUDED.connections_avg,
			connections_max = EXCLUDED.connections_max,
			active_users_avg = EXCLUDED.active_users_avg,
			active_users_max = EXCLUDED.active_users_max,
			dc_coverage_min = EXCLUDED.dc_coverage_min,
			dc_coverage_avg = EXCLUDED.dc_coverage_avg,
			sample_count = EXCLUDED.sample_count
	`, bucketStart, bucketStart, bucketEnd)
	return err
}

func (s *Store) ListServerLoadHourly(ctx context.Context, agentID string, from time.Time, to time.Time) ([]storage.ServerLoadHourlyRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT agent_id, bucket_hour_unix,
			cpu_pct_avg, cpu_pct_max, mem_pct_avg, mem_pct_max,
			connections_avg, connections_max, active_users_avg, active_users_max,
			dc_coverage_min, dc_coverage_avg, sample_count
		FROM ts_server_load_hourly
		WHERE agent_id = ? AND bucket_hour_unix >= ? AND bucket_hour_unix <= ?
		ORDER BY bucket_hour_unix
	`, agentID, toUnix(from), toUnix(to))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []storage.ServerLoadHourlyRecord
	for rows.Next() {
		var r storage.ServerLoadHourlyRecord
		var bucketUnix int64
		if err := rows.Scan(
			&r.AgentID, &bucketUnix,
			&r.CPUPctAvg, &r.CPUPctMax, &r.MemPctAvg, &r.MemPctMax,
			&r.ConnectionsAvg, &r.ConnectionsMax, &r.ActiveUsersAvg, &r.ActiveUsersMax,
			&r.DCCoverageMin, &r.DCCoverageAvg, &r.SampleCount,
		); err != nil {
			return nil, err
		}
		r.BucketHour = fromUnix(bucketUnix)
		result = append(result, r)
	}
	return result, rows.Err()
}

func (s *Store) PruneServerLoadHourly(ctx context.Context, olderThan time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM ts_server_load_hourly WHERE bucket_hour_unix < ?`, toUnix(olderThan))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
