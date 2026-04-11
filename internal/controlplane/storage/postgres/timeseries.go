package postgres

import (
	"context"
	"time"

	"github.com/panvex/panvex/internal/controlplane/storage"
)

func (s *Store) AppendServerLoadPoint(ctx context.Context, record storage.ServerLoadPointRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO ts_server_load (
			agent_id, captured_at,
			cpu_pct_avg, cpu_pct_max, mem_pct_avg, mem_pct_max,
			disk_pct_avg, disk_pct_max, load_1m, load_5m, load_15m,
			connections_avg, connections_max, connections_me_avg, connections_direct_avg,
			active_users_avg, active_users_max,
			connections_total, connections_bad_total, handshake_timeouts_total,
			dc_coverage_min_pct, dc_coverage_avg_pct,
			healthy_upstreams, total_upstreams, net_bytes_sent, net_bytes_recv, sample_count
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27)
		ON CONFLICT (agent_id, captured_at) DO NOTHING
	`,
		record.AgentID, record.CapturedAt.UTC(),
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
		SELECT agent_id, captured_at,
			cpu_pct_avg, cpu_pct_max, mem_pct_avg, mem_pct_max,
			disk_pct_avg, disk_pct_max, load_1m, load_5m, load_15m,
			connections_avg, connections_max, connections_me_avg, connections_direct_avg,
			active_users_avg, active_users_max,
			connections_total, connections_bad_total, handshake_timeouts_total,
			dc_coverage_min_pct, dc_coverage_avg_pct,
			healthy_upstreams, total_upstreams, net_bytes_sent, net_bytes_recv, sample_count
		FROM ts_server_load
		WHERE agent_id = $1 AND captured_at >= $2 AND captured_at <= $3
		ORDER BY captured_at
	`, agentID, from.UTC(), to.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []storage.ServerLoadPointRecord
	for rows.Next() {
		var r storage.ServerLoadPointRecord
		if err := rows.Scan(
			&r.AgentID, &r.CapturedAt,
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
		r.CapturedAt = r.CapturedAt.UTC()
		result = append(result, r)
	}
	return result, rows.Err()
}

func (s *Store) PruneServerLoadPoints(ctx context.Context, olderThan time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM ts_server_load WHERE captured_at < $1`, olderThan.UTC())
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *Store) AppendDCHealthPoint(ctx context.Context, record storage.DCHealthPointRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO ts_dc_health (
			agent_id, captured_at, dc,
			coverage_pct_avg, coverage_pct_min, rtt_ms_avg, rtt_ms_max,
			alive_writers_min, required_writers, load_max, sample_count
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (agent_id, dc, captured_at) DO NOTHING
	`,
		record.AgentID, record.CapturedAt.UTC(), record.DC,
		record.CoveragePctAvg, record.CoveragePctMin, record.RTTMsAvg, record.RTTMsMax,
		record.AliveWritersMin, record.RequiredWriters, record.LoadMax, record.SampleCount,
	)
	return err
}

func (s *Store) ListDCHealthPoints(ctx context.Context, agentID string, from time.Time, to time.Time) ([]storage.DCHealthPointRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT agent_id, captured_at, dc,
			coverage_pct_avg, coverage_pct_min, rtt_ms_avg, rtt_ms_max,
			alive_writers_min, required_writers, load_max, sample_count
		FROM ts_dc_health
		WHERE agent_id = $1 AND captured_at >= $2 AND captured_at <= $3
		ORDER BY dc, captured_at
	`, agentID, from.UTC(), to.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []storage.DCHealthPointRecord
	for rows.Next() {
		var r storage.DCHealthPointRecord
		if err := rows.Scan(
			&r.AgentID, &r.CapturedAt, &r.DC,
			&r.CoveragePctAvg, &r.CoveragePctMin, &r.RTTMsAvg, &r.RTTMsMax,
			&r.AliveWritersMin, &r.RequiredWriters, &r.LoadMax, &r.SampleCount,
		); err != nil {
			return nil, err
		}
		r.CapturedAt = r.CapturedAt.UTC()
		result = append(result, r)
	}
	return result, rows.Err()
}

func (s *Store) PruneDCHealthPoints(ctx context.Context, olderThan time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM ts_dc_health WHERE captured_at < $1`, olderThan.UTC())
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *Store) UpsertClientIPHistory(ctx context.Context, record storage.ClientIPHistoryRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO client_ip_history (agent_id, client_id, ip_address, first_seen, last_seen)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (agent_id, client_id, ip_address) DO UPDATE
		SET last_seen = EXCLUDED.last_seen
	`, record.AgentID, record.ClientID, record.IPAddress, record.FirstSeen.UTC(), record.LastSeen.UTC())
	return err
}

func (s *Store) ListClientIPHistory(ctx context.Context, clientID string, from time.Time, to time.Time) ([]storage.ClientIPHistoryRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT agent_id, client_id, ip_address, first_seen, last_seen
		FROM client_ip_history
		WHERE client_id = $1 AND last_seen >= $2 AND first_seen <= $3
		ORDER BY last_seen DESC
	`, clientID, from.UTC(), to.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []storage.ClientIPHistoryRecord
	for rows.Next() {
		var r storage.ClientIPHistoryRecord
		if err := rows.Scan(&r.AgentID, &r.ClientID, &r.IPAddress, &r.FirstSeen, &r.LastSeen); err != nil {
			return nil, err
		}
		r.FirstSeen = r.FirstSeen.UTC()
		r.LastSeen = r.LastSeen.UTC()
		result = append(result, r)
	}
	return result, rows.Err()
}

func (s *Store) CountUniqueClientIPs(ctx context.Context, clientID string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT ip_address) FROM client_ip_history WHERE client_id = $1`, clientID).Scan(&count)
	return count, err
}

func (s *Store) PruneClientIPHistory(ctx context.Context, olderThan time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM client_ip_history WHERE last_seen < $1`, olderThan.UTC())
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *Store) RollupServerLoadHourly(ctx context.Context, bucketHour time.Time) error {
	bucketStart := bucketHour.Truncate(time.Hour).UTC()
	bucketEnd := bucketStart.Add(time.Hour)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO ts_server_load_hourly (
			agent_id, bucket_hour,
			cpu_pct_avg, cpu_pct_max, mem_pct_avg, mem_pct_max,
			connections_avg, connections_max, active_users_avg, active_users_max,
			dc_coverage_min, dc_coverage_avg, sample_count
		)
		SELECT agent_id, $1,
			AVG(cpu_pct_avg), MAX(cpu_pct_max), AVG(mem_pct_avg), MAX(mem_pct_max),
			AVG(connections_avg), MAX(connections_max), AVG(active_users_avg), MAX(active_users_max),
			MIN(dc_coverage_min_pct), AVG(dc_coverage_avg_pct), COUNT(*)
		FROM ts_server_load
		WHERE captured_at >= $1 AND captured_at < $2
		GROUP BY agent_id
		ON CONFLICT (agent_id, bucket_hour) DO UPDATE SET
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
	`, bucketStart, bucketEnd)
	return err
}

func (s *Store) ListServerLoadHourly(ctx context.Context, agentID string, from time.Time, to time.Time) ([]storage.ServerLoadHourlyRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT agent_id, bucket_hour,
			cpu_pct_avg, cpu_pct_max, mem_pct_avg, mem_pct_max,
			connections_avg, connections_max, active_users_avg, active_users_max,
			dc_coverage_min, dc_coverage_avg, sample_count
		FROM ts_server_load_hourly
		WHERE agent_id = $1 AND bucket_hour >= $2 AND bucket_hour <= $3
		ORDER BY bucket_hour
	`, agentID, from.UTC(), to.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []storage.ServerLoadHourlyRecord
	for rows.Next() {
		var r storage.ServerLoadHourlyRecord
		if err := rows.Scan(
			&r.AgentID, &r.BucketHour,
			&r.CPUPctAvg, &r.CPUPctMax, &r.MemPctAvg, &r.MemPctMax,
			&r.ConnectionsAvg, &r.ConnectionsMax, &r.ActiveUsersAvg, &r.ActiveUsersMax,
			&r.DCCoverageMin, &r.DCCoverageAvg, &r.SampleCount,
		); err != nil {
			return nil, err
		}
		r.BucketHour = r.BucketHour.UTC()
		result = append(result, r)
	}
	return result, rows.Err()
}

func (s *Store) PruneServerLoadHourly(ctx context.Context, olderThan time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM ts_server_load_hourly WHERE bucket_hour < $1`, olderThan.UTC())
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
