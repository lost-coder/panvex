package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
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

// pruneChunkSize bounds a single PruneServerLoadPoints DELETE so we
// never hold a giant lock while the timeseries thinning job catches up
// after a long downtime. Sized to fit comfortably in one PG WAL segment
// even with the widest row layout (~28 columns).
const pruneChunkSize = 10_000

// pruneMaxIterations caps the number of chunks one Prune call will
// burn through. With chunk=10k that drops up to 50M rows per invocation
// before yielding to the next scheduler tick — anything larger is the
// retention scheduler's job, not a single prune call's.
const pruneMaxIterations = 5_000

func (s *Store) PruneServerLoadPoints(ctx context.Context, olderThan time.Time) (int64, error) {
	// Chunk so retention catch-up after a long pause does not lock
	// ts_server_load for an unbounded interval. Each iteration is a
	// short DELETE of at most pruneChunkSize rows, terminating when
	// either no candidate rows remain or the iteration cap fires.
	cutoff := olderThan.UTC()
	var total int64
	for i := 0; i < pruneMaxIterations; i++ {
		result, err := s.db.ExecContext(ctx, `
			DELETE FROM ts_server_load
			WHERE ctid IN (
				SELECT ctid FROM ts_server_load
				WHERE captured_at < $1
				LIMIT $2
			)
		`, cutoff, pruneChunkSize)
		if err != nil {
			return total, err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return total, err
		}
		total += affected
		if affected < pruneChunkSize {
			return total, nil
		}
	}
	return total, nil
}

// listServerLoadPointsForAgentsChunkSize bounds the IN-list per
// SQL round-trip. Postgres caps the bind-parameter count at 65535
// (16-bit message field); 250 leaves plenty of headroom relative to
// the two `from`/`to` parameters tacked onto every query.
const listServerLoadPointsForAgentsChunkSize = 250

// ListServerLoadPointsForAgents returns load points for a batch of
// agents (Q2.U-P-01). Each agent's slice is sorted by captured_at
// ascending; missing agents are absent from the map. Chunked so the
// IN-list never approaches the Postgres 65535-parameter ceiling.
func (s *Store) ListServerLoadPointsForAgents(ctx context.Context, agentIDs []string, from time.Time, to time.Time) (map[string][]storage.ServerLoadPointRecord, error) {
	out := make(map[string][]storage.ServerLoadPointRecord, len(agentIDs))
	if len(agentIDs) == 0 {
		return out, nil
	}
	for start := 0; start < len(agentIDs); start += listServerLoadPointsForAgentsChunkSize {
		end := start + listServerLoadPointsForAgentsChunkSize
		if end > len(agentIDs) {
			end = len(agentIDs)
		}
		if err := s.appendServerLoadPointsChunk(ctx, agentIDs[start:end], from, to, out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Store) appendServerLoadPointsChunk(ctx context.Context, chunk []string, from, to time.Time, out map[string][]storage.ServerLoadPointRecord) error {
	placeholders := make([]string, len(chunk))
	args := make([]any, 0, len(chunk)+2)
	for i, id := range chunk {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args = append(args, id)
	}
	args = append(args, from.UTC(), to.UTC())
	fromIdx := len(chunk) + 1
	toIdx := len(chunk) + 2
	query := fmt.Sprintf(`
		SELECT agent_id, captured_at,
			cpu_pct_avg, cpu_pct_max, mem_pct_avg, mem_pct_max,
			disk_pct_avg, disk_pct_max, load_1m, load_5m, load_15m,
			connections_avg, connections_max, connections_me_avg, connections_direct_avg,
			active_users_avg, active_users_max,
			connections_total, connections_bad_total, handshake_timeouts_total,
			dc_coverage_min_pct, dc_coverage_avg_pct,
			healthy_upstreams, total_upstreams, net_bytes_sent, net_bytes_recv, sample_count
		FROM ts_server_load
		WHERE agent_id IN (%s) AND captured_at >= $%d AND captured_at <= $%d
		ORDER BY agent_id, captured_at
	`, strings.Join(placeholders, ","), fromIdx, toIdx)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
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
			return err
		}
		r.CapturedAt = r.CapturedAt.UTC()
		out[r.AgentID] = append(out[r.AgentID], r)
	}
	return rows.Err()
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

// ListClientIPHistory returns the per-(agent, ip) seen rows for `clientID`
// inside the time window [from, to]. Capped at storage.DefaultListLimit
// rows (P-7) so a high-cardinality client cannot stream millions of rows
// when a caller forgets to pre-aggregate. Operators that genuinely need
// every row should use AggregateClientIPHistory or a cursor-paginated
// follow-up query.
func (s *Store) ListClientIPHistory(ctx context.Context, clientID string, from time.Time, to time.Time) ([]storage.ClientIPHistoryRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT agent_id, client_id, ip_address, first_seen, last_seen
		FROM client_ip_history
		WHERE client_id = $1 AND last_seen >= $2 AND first_seen <= $3
		ORDER BY last_seen DESC
		LIMIT $4
	`, clientID, from.UTC(), to.UTC(), storage.DefaultListLimit)
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

// AggregateClientIPHistory pushes the per-IP fold into the database:
// one row per IP, with MIN(first_seen) / MAX(last_seen) across all
// agents that reported it. Limit is applied in SQL so a high-cardinality
// client never streams millions of raw rows back to the control plane.
// A zero or negative limit disables the cap.
func (s *Store) AggregateClientIPHistory(ctx context.Context, clientID string, from time.Time, to time.Time, limit int) ([]storage.ClientIPAggregateRecord, error) {
	query := `
		SELECT ip_address, MIN(first_seen) AS first_seen, MAX(last_seen) AS last_seen
		FROM client_ip_history
		WHERE client_id = $1 AND last_seen >= $2 AND first_seen <= $3
		GROUP BY ip_address
		ORDER BY last_seen DESC
	`
	args := []any{clientID, from.UTC(), to.UTC()}
	if limit > 0 {
		query += " LIMIT $4"
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
		if err := rows.Scan(&r.IPAddress, &r.FirstSeen, &r.LastSeen); err != nil {
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
		placeholders[i] = fmt.Sprintf("$%d", i+1)
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
			COALESCE(SUM(cpu_pct_avg * sample_count) * 1.0 / NULLIF(SUM(sample_count), 0), 0), MAX(cpu_pct_max),
			COALESCE(SUM(mem_pct_avg * sample_count) * 1.0 / NULLIF(SUM(sample_count), 0), 0), MAX(mem_pct_max),
			COALESCE(SUM(connections_avg * sample_count) * 1.0 / NULLIF(SUM(sample_count), 0), 0), MAX(connections_max),
			COALESCE(SUM(active_users_avg * sample_count) * 1.0 / NULLIF(SUM(sample_count), 0), 0), MAX(active_users_max),
			MIN(dc_coverage_min_pct), COALESCE(SUM(dc_coverage_avg_pct * sample_count) * 1.0 / NULLIF(SUM(sample_count), 0), 0), SUM(sample_count)
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
