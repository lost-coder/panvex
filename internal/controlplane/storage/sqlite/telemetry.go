package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

func (s *Store) PutTelemetryRuntimeCurrent(ctx context.Context, record storage.TelemetryRuntimeCurrentRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO telemt_runtime_current (agent_id, observed_at_unix, runtime_json)
		VALUES (?, ?, ?)
		ON CONFLICT(agent_id) DO UPDATE SET
			observed_at_unix = excluded.observed_at_unix,
			runtime_json = excluded.runtime_json
	`, record.AgentID, toUnix(record.ObservedAt), record.RuntimeJSON)
	return err
}

func (s *Store) GetTelemetryRuntimeCurrent(ctx context.Context, agentID string) (storage.TelemetryRuntimeCurrentRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT agent_id, observed_at_unix, runtime_json
		FROM telemt_runtime_current
		WHERE agent_id = ?
	`, agentID)

	var record storage.TelemetryRuntimeCurrentRecord
	var observedAt int64
	if err := row.Scan(&record.AgentID, &observedAt, &record.RuntimeJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.TelemetryRuntimeCurrentRecord{}, storage.ErrNotFound
		}
		return storage.TelemetryRuntimeCurrentRecord{}, err
	}
	record.ObservedAt = fromUnix(observedAt)
	return record, nil
}

func (s *Store) ListTelemetryRuntimeCurrent(ctx context.Context) ([]storage.TelemetryRuntimeCurrentRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT agent_id, observed_at_unix, runtime_json
		FROM telemt_runtime_current
		ORDER BY observed_at_unix, agent_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.TelemetryRuntimeCurrentRecord, 0)
	for rows.Next() {
		var record storage.TelemetryRuntimeCurrentRecord
		var observedAt int64
		if err := rows.Scan(&record.AgentID, &observedAt, &record.RuntimeJSON); err != nil {
			return nil, err
		}
		record.ObservedAt = fromUnix(observedAt)
		result = append(result, record)
	}
	return result, rows.Err()
}

func (s *Store) ReplaceTelemetryRuntimeDCs(ctx context.Context, agentID string, records []storage.TelemetryRuntimeDCRecord) error {
	tx, err := s.beginInternalTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM telemt_runtime_dcs_current WHERE agent_id = ?`, agentID); err != nil {
		return err
	}
	for _, record := range records {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO telemt_runtime_dcs_current (
				agent_id, dc, observed_at_unix, available_endpoints, available_pct,
				required_writers, alive_writers, coverage_pct, rtt_ms, load
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, record.AgentID, record.DC, toUnix(record.ObservedAt), record.AvailableEndpoints, record.AvailablePct,
			record.RequiredWriters, record.AliveWriters, record.CoveragePct, record.RTTMs, record.Load); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *Store) ListTelemetryRuntimeDCs(ctx context.Context, agentID string) ([]storage.TelemetryRuntimeDCRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT agent_id, dc, observed_at_unix, available_endpoints, available_pct,
		       required_writers, alive_writers, coverage_pct, rtt_ms, load
		FROM telemt_runtime_dcs_current
		WHERE agent_id = ?
		ORDER BY dc
	`, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.TelemetryRuntimeDCRecord, 0)
	for rows.Next() {
		var record storage.TelemetryRuntimeDCRecord
		var observedAt int64
		if err := rows.Scan(&record.AgentID, &record.DC, &observedAt, &record.AvailableEndpoints, &record.AvailablePct,
			&record.RequiredWriters, &record.AliveWriters, &record.CoveragePct, &record.RTTMs, &record.Load); err != nil {
			return nil, err
		}
		record.ObservedAt = fromUnix(observedAt)
		result = append(result, record)
	}

	return result, rows.Err()
}

// ListAllTelemetryRuntimeDCs returns DC rows for every agent in a single
// query so cold-start rehydration groups by agent_id in memory instead of
// issuing one query per agent (A2).
func (s *Store) ListAllTelemetryRuntimeDCs(ctx context.Context) ([]storage.TelemetryRuntimeDCRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT agent_id, dc, observed_at_unix, available_endpoints, available_pct,
		       required_writers, alive_writers, coverage_pct, rtt_ms, load
		FROM telemt_runtime_dcs_current
		ORDER BY agent_id, dc
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.TelemetryRuntimeDCRecord, 0)
	for rows.Next() {
		var record storage.TelemetryRuntimeDCRecord
		var observedAt int64
		if err := rows.Scan(&record.AgentID, &record.DC, &observedAt, &record.AvailableEndpoints, &record.AvailablePct,
			&record.RequiredWriters, &record.AliveWriters, &record.CoveragePct, &record.RTTMs, &record.Load); err != nil {
			return nil, err
		}
		record.ObservedAt = fromUnix(observedAt)
		result = append(result, record)
	}

	return result, rows.Err()
}

func (s *Store) ReplaceTelemetryRuntimeUpstreams(ctx context.Context, agentID string, records []storage.TelemetryRuntimeUpstreamRecord) error {
	tx, err := s.beginInternalTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM telemt_runtime_upstreams_current WHERE agent_id = ?`, agentID); err != nil {
		return err
	}
	for _, record := range records {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO telemt_runtime_upstreams_current (
				agent_id, upstream_id, observed_at_unix, route_kind, address, healthy, fails, effective_latency_ms
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`, record.AgentID, record.UpstreamID, toUnix(record.ObservedAt), record.RouteKind, record.Address,
			boolToInt(record.Healthy), record.Fails, record.EffectiveLatencyMs); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *Store) ListTelemetryRuntimeUpstreams(ctx context.Context, agentID string) ([]storage.TelemetryRuntimeUpstreamRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT agent_id, upstream_id, observed_at_unix, route_kind, address, healthy, fails, effective_latency_ms
		FROM telemt_runtime_upstreams_current
		WHERE agent_id = ?
		ORDER BY upstream_id
	`, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.TelemetryRuntimeUpstreamRecord, 0)
	for rows.Next() {
		var record storage.TelemetryRuntimeUpstreamRecord
		var observedAt int64
		var healthy int
		if err := rows.Scan(&record.AgentID, &record.UpstreamID, &observedAt, &record.RouteKind, &record.Address, &healthy, &record.Fails, &record.EffectiveLatencyMs); err != nil {
			return nil, err
		}
		record.ObservedAt = fromUnix(observedAt)
		record.Healthy = intToBool(healthy)
		result = append(result, record)
	}

	return result, rows.Err()
}

// ListAllTelemetryRuntimeUpstreams returns upstream rows for every agent
// in a single query (A2 cold-start rehydration).
func (s *Store) ListAllTelemetryRuntimeUpstreams(ctx context.Context) ([]storage.TelemetryRuntimeUpstreamRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT agent_id, upstream_id, observed_at_unix, route_kind, address, healthy, fails, effective_latency_ms
		FROM telemt_runtime_upstreams_current
		ORDER BY agent_id, upstream_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.TelemetryRuntimeUpstreamRecord, 0)
	for rows.Next() {
		var record storage.TelemetryRuntimeUpstreamRecord
		var observedAt int64
		var healthy int
		if err := rows.Scan(&record.AgentID, &record.UpstreamID, &observedAt, &record.RouteKind, &record.Address, &healthy, &record.Fails, &record.EffectiveLatencyMs); err != nil {
			return nil, err
		}
		record.ObservedAt = fromUnix(observedAt)
		record.Healthy = intToBool(healthy)
		result = append(result, record)
	}

	return result, rows.Err()
}

// AppendTelemetryRuntimeEvents persists runtime events for an agent.
// Phase-2 §2.4: was a per-row loop inside one transaction; now a true
// multi-row INSERT (chunked at bulkChunkSize) so a single agent posting
// hundreds of events per second does not pay one round-trip per row.
// ON CONFLICT semantics are preserved exactly: duplicate (agent_id,
// sequence) rows update the four payload columns and the
// observed_at_unix timestamp.
func (s *Store) AppendTelemetryRuntimeEvents(ctx context.Context, agentID string, records []storage.TelemetryRuntimeEventRecord) error {
	if len(records) == 0 {
		return nil
	}
	const cols = 7
	return s.execInTx(ctx, func(exec dbExecutor) error {
		for start := 0; start < len(records); start += bulkChunkSize {
			end := start + bulkChunkSize
			if end > len(records) {
				end = len(records)
			}
			chunk := records[start:end]
			args := make([]any, 0, len(chunk)*cols)
			for _, record := range chunk {
				args = append(args,
					agentID, record.Sequence,
					toUnix(record.ObservedAt), toUnix(record.Timestamp),
					record.EventType, record.Context, record.Severity,
				)
			}
			query := fmt.Sprintf(`
				INSERT INTO telemt_runtime_events (
					agent_id, sequence, observed_at_unix, timestamp_unix, event_type, context, severity
				)
				VALUES %s
				ON CONFLICT(agent_id, sequence) DO UPDATE SET
					observed_at_unix = excluded.observed_at_unix,
					timestamp_unix = excluded.timestamp_unix,
					event_type = excluded.event_type,
					context = excluded.context,
					severity = excluded.severity
			`, rowPlaceholders(len(chunk), cols))
			if _, err := exec.ExecContext(ctx, query, args...); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) ListTelemetryRuntimeEvents(ctx context.Context, agentID string, limit int) ([]storage.TelemetryRuntimeEventRecord, error) {
	query := `
		SELECT agent_id, sequence, observed_at_unix, timestamp_unix, event_type, context, severity
		FROM telemt_runtime_events
		WHERE agent_id = ?
		ORDER BY timestamp_unix DESC, sequence DESC
	`
	var rows *sql.Rows
	var err error
	if limit > 0 {
		query += ` LIMIT ?`
		rows, err = s.db.QueryContext(ctx, query, agentID, limit)
	} else {
		rows, err = s.db.QueryContext(ctx, query, agentID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.TelemetryRuntimeEventRecord, 0)
	for rows.Next() {
		var record storage.TelemetryRuntimeEventRecord
		var observedAt int64
		var timestamp int64
		if err := rows.Scan(&record.AgentID, &record.Sequence, &observedAt, &timestamp, &record.EventType, &record.Context, &record.Severity); err != nil {
			return nil, err
		}
		record.ObservedAt = fromUnix(observedAt)
		record.Timestamp = fromUnix(timestamp)
		result = append(result, record)
	}

	return result, rows.Err()
}

// ListAllTelemetryRuntimeEventsPerAgent returns the most recent
// perAgentLimit events PER agent for every agent in one query. The
// per-agent window is enforced by ROW_NUMBER() OVER (PARTITION BY
// agent_id ...) — NOT a global LIMIT — so an agent with 100 events and an
// agent with 3 each get their own newest-N slice. modernc.org/sqlite
// supports window functions. perAgentLimit <= 0 returns all events.
func (s *Store) ListAllTelemetryRuntimeEventsPerAgent(ctx context.Context, perAgentLimit int) ([]storage.TelemetryRuntimeEventRecord, error) {
	var rows *sql.Rows
	var err error
	if perAgentLimit > 0 {
		rows, err = s.db.QueryContext(ctx, `
			SELECT agent_id, sequence, observed_at_unix, timestamp_unix, event_type, context, severity
			FROM (
				SELECT agent_id, sequence, observed_at_unix, timestamp_unix, event_type, context, severity,
				       ROW_NUMBER() OVER (PARTITION BY agent_id ORDER BY timestamp_unix DESC, sequence DESC) AS rn
				FROM telemt_runtime_events
			)
			WHERE rn <= ?
			ORDER BY agent_id, timestamp_unix DESC, sequence DESC
		`, perAgentLimit)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT agent_id, sequence, observed_at_unix, timestamp_unix, event_type, context, severity
			FROM telemt_runtime_events
			ORDER BY agent_id, timestamp_unix DESC, sequence DESC
		`)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.TelemetryRuntimeEventRecord, 0)
	for rows.Next() {
		var record storage.TelemetryRuntimeEventRecord
		var observedAt int64
		var timestamp int64
		if err := rows.Scan(&record.AgentID, &record.Sequence, &observedAt, &timestamp, &record.EventType, &record.Context, &record.Severity); err != nil {
			return nil, err
		}
		record.ObservedAt = fromUnix(observedAt)
		record.Timestamp = fromUnix(timestamp)
		result = append(result, record)
	}

	return result, rows.Err()
}

func (s *Store) PruneTelemetryRuntimeEvents(ctx context.Context, olderThan time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM telemt_runtime_events WHERE timestamp_unix < ?`, toUnix(olderThan))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *Store) PutTelemetryDiagnosticsCurrent(ctx context.Context, record storage.TelemetryDiagnosticsCurrentRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO telemt_diagnostics_current (
			agent_id, observed_at_unix, state, state_reason, system_info_json,
			effective_limits_json, security_posture_json, minimal_all_json, me_pool_json, dcs_json
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(agent_id) DO UPDATE SET
			observed_at_unix = excluded.observed_at_unix,
			state = excluded.state,
			state_reason = excluded.state_reason,
			system_info_json = excluded.system_info_json,
			effective_limits_json = excluded.effective_limits_json,
			security_posture_json = excluded.security_posture_json,
			minimal_all_json = excluded.minimal_all_json,
			me_pool_json = excluded.me_pool_json,
			dcs_json = excluded.dcs_json
	`, record.AgentID, toUnix(record.ObservedAt), record.State, record.StateReason, record.SystemInfoJSON,
		record.EffectiveLimitsJSON, record.SecurityPostureJSON, record.MinimalAllJSON, record.MEPoolJSON, record.DcsJSON)
	return err
}

func (s *Store) GetTelemetryDiagnosticsCurrent(ctx context.Context, agentID string) (storage.TelemetryDiagnosticsCurrentRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT agent_id, observed_at_unix, state, state_reason, system_info_json,
		       effective_limits_json, security_posture_json, minimal_all_json, me_pool_json, dcs_json
		FROM telemt_diagnostics_current
		WHERE agent_id = ?
	`, agentID)

	var record storage.TelemetryDiagnosticsCurrentRecord
	var observedAt int64
	if err := row.Scan(&record.AgentID, &observedAt, &record.State, &record.StateReason, &record.SystemInfoJSON,
		&record.EffectiveLimitsJSON, &record.SecurityPostureJSON, &record.MinimalAllJSON, &record.MEPoolJSON, &record.DcsJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.TelemetryDiagnosticsCurrentRecord{}, storage.ErrNotFound
		}
		return storage.TelemetryDiagnosticsCurrentRecord{}, err
	}
	record.ObservedAt = fromUnix(observedAt)
	return record, nil
}

func (s *Store) PutTelemetrySecurityInventoryCurrent(ctx context.Context, record storage.TelemetrySecurityInventoryCurrentRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO telemt_security_inventory_current (
			agent_id, observed_at_unix, state, state_reason, enabled, entries_total, entries_json
		)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(agent_id) DO UPDATE SET
			observed_at_unix = excluded.observed_at_unix,
			state = excluded.state,
			state_reason = excluded.state_reason,
			enabled = excluded.enabled,
			entries_total = excluded.entries_total,
			entries_json = excluded.entries_json
	`, record.AgentID, toUnix(record.ObservedAt), record.State, record.StateReason, boolToInt(record.Enabled), record.EntriesTotal, record.EntriesJSON)
	return err
}

func (s *Store) GetTelemetrySecurityInventoryCurrent(ctx context.Context, agentID string) (storage.TelemetrySecurityInventoryCurrentRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT agent_id, observed_at_unix, state, state_reason, enabled, entries_total, entries_json
		FROM telemt_security_inventory_current
		WHERE agent_id = ?
	`, agentID)

	var record storage.TelemetrySecurityInventoryCurrentRecord
	var observedAt int64
	var enabled int
	if err := row.Scan(&record.AgentID, &observedAt, &record.State, &record.StateReason, &enabled, &record.EntriesTotal, &record.EntriesJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.TelemetrySecurityInventoryCurrentRecord{}, storage.ErrNotFound
		}
		return storage.TelemetrySecurityInventoryCurrentRecord{}, err
	}
	record.ObservedAt = fromUnix(observedAt)
	record.Enabled = intToBool(enabled)
	return record, nil
}
