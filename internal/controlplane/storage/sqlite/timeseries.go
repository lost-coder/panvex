package sqlite

import (
	"context"
	"time"

	"github.com/panvex/panvex/internal/controlplane/storage"
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
			healthy_upstreams, total_upstreams, sample_count
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
	`,
		record.AgentID, toUnix(record.CapturedAt),
		record.CPUPctAvg, record.CPUPctMax, record.MemPctAvg, record.MemPctMax,
		record.DiskPctAvg, record.DiskPctMax, record.Load1M, record.Load5M, record.Load15M,
		record.ConnectionsAvg, record.ConnectionsMax, record.ConnectionsMEAvg, record.ConnectionsDirectAvg,
		record.ActiveUsersAvg, record.ActiveUsersMax,
		record.ConnectionsTotal, record.ConnectionsBadTotal, record.HandshakeTimeoutsTotal,
		record.DCCoverageMinPct, record.DCCoverageAvgPct,
		record.HealthyUpstreams, record.TotalUpstreams, record.SampleCount,
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
			healthy_upstreams, total_upstreams, sample_count
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
			&r.HealthyUpstreams, &r.TotalUpstreams, &r.SampleCount,
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

func (s *Store) PruneClientIPHistory(ctx context.Context, olderThan time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM client_ip_history WHERE last_seen_unix < ?`, toUnix(olderThan))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
