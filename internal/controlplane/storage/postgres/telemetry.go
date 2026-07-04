package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

func (s *Store) PutTelemetryRuntimeCurrent(ctx context.Context, record storage.TelemetryRuntimeCurrentRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO telemt_runtime_current (agent_id, observed_at, runtime_json)
		VALUES ($1, $2, $3)
		ON CONFLICT (agent_id) DO UPDATE
		SET observed_at = EXCLUDED.observed_at,
		    runtime_json = EXCLUDED.runtime_json
	`, record.AgentID, record.ObservedAt.UTC(), record.RuntimeJSON)
	return err
}

func (s *Store) GetTelemetryRuntimeCurrent(ctx context.Context, agentID string) (storage.TelemetryRuntimeCurrentRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT agent_id, observed_at, runtime_json
		FROM telemt_runtime_current
		WHERE agent_id = $1
	`, agentID)

	var record storage.TelemetryRuntimeCurrentRecord
	if err := row.Scan(&record.AgentID, &record.ObservedAt, &record.RuntimeJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.TelemetryRuntimeCurrentRecord{}, storage.ErrNotFound
		}
		return storage.TelemetryRuntimeCurrentRecord{}, err
	}
	record.ObservedAt = record.ObservedAt.UTC()
	return record, nil
}

func (s *Store) ListTelemetryRuntimeCurrent(ctx context.Context) ([]storage.TelemetryRuntimeCurrentRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT agent_id, observed_at, runtime_json
		FROM telemt_runtime_current
		ORDER BY observed_at, agent_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.TelemetryRuntimeCurrentRecord, 0)
	for rows.Next() {
		var record storage.TelemetryRuntimeCurrentRecord
		if err := rows.Scan(&record.AgentID, &record.ObservedAt, &record.RuntimeJSON); err != nil {
			return nil, err
		}
		record.ObservedAt = record.ObservedAt.UTC()
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

	if _, err := tx.ExecContext(ctx, `DELETE FROM telemt_runtime_dcs_current WHERE agent_id = $1`, agentID); err != nil {
		return err
	}
	for _, record := range records {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO telemt_runtime_dcs_current (
				agent_id, dc, observed_at, available_endpoints, available_pct,
				required_writers, alive_writers, coverage_pct, rtt_ms, load
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		`, record.AgentID, record.DC, record.ObservedAt.UTC(), record.AvailableEndpoints, record.AvailablePct,
			record.RequiredWriters, record.AliveWriters, record.CoveragePct, record.RTTMs, record.Load); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *Store) ListTelemetryRuntimeDCs(ctx context.Context, agentID string) ([]storage.TelemetryRuntimeDCRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT agent_id, dc, observed_at, available_endpoints, available_pct,
		       required_writers, alive_writers, coverage_pct, rtt_ms, load
		FROM telemt_runtime_dcs_current
		WHERE agent_id = $1
		ORDER BY dc
	`, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.TelemetryRuntimeDCRecord, 0)
	for rows.Next() {
		var record storage.TelemetryRuntimeDCRecord
		if err := rows.Scan(&record.AgentID, &record.DC, &record.ObservedAt, &record.AvailableEndpoints, &record.AvailablePct,
			&record.RequiredWriters, &record.AliveWriters, &record.CoveragePct, &record.RTTMs, &record.Load); err != nil {
			return nil, err
		}
		record.ObservedAt = record.ObservedAt.UTC()
		result = append(result, record)
	}

	return result, rows.Err()
}

// ListAllTelemetryRuntimeDCs returns DC rows for every agent in a single
// query so cold-start rehydration groups by agent_id in memory instead of
// issuing one query per agent (A2).
func (s *Store) ListAllTelemetryRuntimeDCs(ctx context.Context) ([]storage.TelemetryRuntimeDCRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT agent_id, dc, observed_at, available_endpoints, available_pct,
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
		if err := rows.Scan(&record.AgentID, &record.DC, &record.ObservedAt, &record.AvailableEndpoints, &record.AvailablePct,
			&record.RequiredWriters, &record.AliveWriters, &record.CoveragePct, &record.RTTMs, &record.Load); err != nil {
			return nil, err
		}
		record.ObservedAt = record.ObservedAt.UTC()
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

	if _, err := tx.ExecContext(ctx, `DELETE FROM telemt_runtime_upstreams_current WHERE agent_id = $1`, agentID); err != nil {
		return err
	}
	for _, record := range records {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO telemt_runtime_upstreams_current (
				agent_id, upstream_id, observed_at, route_kind, address, healthy, fails, effective_latency_ms
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		`, record.AgentID, record.UpstreamID, record.ObservedAt.UTC(), record.RouteKind, record.Address, record.Healthy, record.Fails, record.EffectiveLatencyMs); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *Store) ListTelemetryRuntimeUpstreams(ctx context.Context, agentID string) ([]storage.TelemetryRuntimeUpstreamRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT agent_id, upstream_id, observed_at, route_kind, address, healthy, fails, effective_latency_ms
		FROM telemt_runtime_upstreams_current
		WHERE agent_id = $1
		ORDER BY upstream_id
	`, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.TelemetryRuntimeUpstreamRecord, 0)
	for rows.Next() {
		var record storage.TelemetryRuntimeUpstreamRecord
		if err := rows.Scan(&record.AgentID, &record.UpstreamID, &record.ObservedAt, &record.RouteKind, &record.Address, &record.Healthy, &record.Fails, &record.EffectiveLatencyMs); err != nil {
			return nil, err
		}
		record.ObservedAt = record.ObservedAt.UTC()
		result = append(result, record)
	}

	return result, rows.Err()
}

// ListAllTelemetryRuntimeUpstreams returns upstream rows for every agent
// in a single query (A2 cold-start rehydration).
func (s *Store) ListAllTelemetryRuntimeUpstreams(ctx context.Context) ([]storage.TelemetryRuntimeUpstreamRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT agent_id, upstream_id, observed_at, route_kind, address, healthy, fails, effective_latency_ms
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
		if err := rows.Scan(&record.AgentID, &record.UpstreamID, &record.ObservedAt, &record.RouteKind, &record.Address, &record.Healthy, &record.Fails, &record.EffectiveLatencyMs); err != nil {
			return nil, err
		}
		record.ObservedAt = record.ObservedAt.UTC()
		result = append(result, record)
	}

	return result, rows.Err()
}

func (s *Store) AppendTelemetryRuntimeEvents(ctx context.Context, agentID string, records []storage.TelemetryRuntimeEventRecord) error {
	tx, err := s.beginInternalTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, record := range records {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO telemt_runtime_events (agent_id, sequence, observed_at, timestamp_at, event_type, context, severity)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (agent_id, sequence) DO UPDATE
			SET observed_at = EXCLUDED.observed_at,
			    timestamp_at = EXCLUDED.timestamp_at,
			    event_type = EXCLUDED.event_type,
			    context = EXCLUDED.context,
			    severity = EXCLUDED.severity
		`, agentID, record.Sequence, record.ObservedAt.UTC(), record.Timestamp.UTC(), record.EventType, record.Context, record.Severity); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *Store) ListTelemetryRuntimeEvents(ctx context.Context, agentID string, limit int) ([]storage.TelemetryRuntimeEventRecord, error) {
	query := `
		SELECT agent_id, sequence, observed_at, timestamp_at, event_type, context, severity
		FROM telemt_runtime_events
		WHERE agent_id = $1
		ORDER BY timestamp_at DESC, sequence DESC
	`
	var rows *sql.Rows
	var err error
	if limit > 0 {
		query += ` LIMIT $2`
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
		if err := rows.Scan(&record.AgentID, &record.Sequence, &record.ObservedAt, &record.Timestamp, &record.EventType, &record.Context, &record.Severity); err != nil {
			return nil, err
		}
		record.ObservedAt = record.ObservedAt.UTC()
		record.Timestamp = record.Timestamp.UTC()
		result = append(result, record)
	}

	return result, rows.Err()
}

// ListAllTelemetryRuntimeEventsPerAgent returns the most recent
// perAgentLimit events PER agent for every agent in one query. The
// per-agent window is enforced by ROW_NUMBER() OVER (PARTITION BY
// agent_id ...) — NOT a global LIMIT — so each agent gets its own
// newest-N slice. perAgentLimit <= 0 returns all events.
func (s *Store) ListAllTelemetryRuntimeEventsPerAgent(ctx context.Context, perAgentLimit int) ([]storage.TelemetryRuntimeEventRecord, error) {
	var rows *sql.Rows
	var err error
	if perAgentLimit > 0 {
		rows, err = s.db.QueryContext(ctx, `
			SELECT agent_id, sequence, observed_at, timestamp_at, event_type, context, severity
			FROM (
				SELECT agent_id, sequence, observed_at, timestamp_at, event_type, context, severity,
				       ROW_NUMBER() OVER (PARTITION BY agent_id ORDER BY timestamp_at DESC, sequence DESC) AS rn
				FROM telemt_runtime_events
			) windowed
			WHERE rn <= $1
			ORDER BY agent_id, timestamp_at DESC, sequence DESC
		`, perAgentLimit)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT agent_id, sequence, observed_at, timestamp_at, event_type, context, severity
			FROM telemt_runtime_events
			ORDER BY agent_id, timestamp_at DESC, sequence DESC
		`)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.TelemetryRuntimeEventRecord, 0)
	for rows.Next() {
		var record storage.TelemetryRuntimeEventRecord
		if err := rows.Scan(&record.AgentID, &record.Sequence, &record.ObservedAt, &record.Timestamp, &record.EventType, &record.Context, &record.Severity); err != nil {
			return nil, err
		}
		record.ObservedAt = record.ObservedAt.UTC()
		record.Timestamp = record.Timestamp.UTC()
		result = append(result, record)
	}

	return result, rows.Err()
}

func (s *Store) PruneTelemetryRuntimeEvents(ctx context.Context, olderThan time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM telemt_runtime_events WHERE timestamp_at < $1`, olderThan.UTC())
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *Store) PutTelemetryDiagnosticsCurrent(ctx context.Context, record storage.TelemetryDiagnosticsCurrentRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO telemt_diagnostics_current (
			agent_id, observed_at, state, state_reason, system_info_json, effective_limits_json,
			security_posture_json, minimal_all_json, me_pool_json, dcs_json
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (agent_id) DO UPDATE
		SET observed_at = EXCLUDED.observed_at,
		    state = EXCLUDED.state,
		    state_reason = EXCLUDED.state_reason,
		    system_info_json = EXCLUDED.system_info_json,
		    effective_limits_json = EXCLUDED.effective_limits_json,
		    security_posture_json = EXCLUDED.security_posture_json,
		    minimal_all_json = EXCLUDED.minimal_all_json,
		    me_pool_json = EXCLUDED.me_pool_json,
		    dcs_json = EXCLUDED.dcs_json
	`, record.AgentID, record.ObservedAt.UTC(), record.State, record.StateReason, record.SystemInfoJSON, record.EffectiveLimitsJSON, record.SecurityPostureJSON, record.MinimalAllJSON, record.MEPoolJSON, record.DcsJSON)
	return err
}

func (s *Store) GetTelemetryDiagnosticsCurrent(ctx context.Context, agentID string) (storage.TelemetryDiagnosticsCurrentRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT agent_id, observed_at, state, state_reason, system_info_json, effective_limits_json,
		       security_posture_json, minimal_all_json, me_pool_json, dcs_json
		FROM telemt_diagnostics_current
		WHERE agent_id = $1
	`, agentID)

	var record storage.TelemetryDiagnosticsCurrentRecord
	if err := row.Scan(&record.AgentID, &record.ObservedAt, &record.State, &record.StateReason, &record.SystemInfoJSON,
		&record.EffectiveLimitsJSON, &record.SecurityPostureJSON, &record.MinimalAllJSON, &record.MEPoolJSON, &record.DcsJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.TelemetryDiagnosticsCurrentRecord{}, storage.ErrNotFound
		}
		return storage.TelemetryDiagnosticsCurrentRecord{}, err
	}
	record.ObservedAt = record.ObservedAt.UTC()
	return record, nil
}

func (s *Store) PutTelemetrySecurityInventoryCurrent(ctx context.Context, record storage.TelemetrySecurityInventoryCurrentRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO telemt_security_inventory_current (
			agent_id, observed_at, state, state_reason, enabled, entries_total, entries_json
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (agent_id) DO UPDATE
		SET observed_at = EXCLUDED.observed_at,
		    state = EXCLUDED.state,
		    state_reason = EXCLUDED.state_reason,
		    enabled = EXCLUDED.enabled,
		    entries_total = EXCLUDED.entries_total,
		    entries_json = EXCLUDED.entries_json
	`, record.AgentID, record.ObservedAt.UTC(), record.State, record.StateReason, record.Enabled, record.EntriesTotal, record.EntriesJSON)
	return err
}

func (s *Store) GetTelemetrySecurityInventoryCurrent(ctx context.Context, agentID string) (storage.TelemetrySecurityInventoryCurrentRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT agent_id, observed_at, state, state_reason, enabled, entries_total, entries_json
		FROM telemt_security_inventory_current
		WHERE agent_id = $1
	`, agentID)

	var record storage.TelemetrySecurityInventoryCurrentRecord
	if err := row.Scan(&record.AgentID, &record.ObservedAt, &record.State, &record.StateReason, &record.Enabled, &record.EntriesTotal, &record.EntriesJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.TelemetrySecurityInventoryCurrentRecord{}, storage.ErrNotFound
		}
		return storage.TelemetrySecurityInventoryCurrentRecord{}, err
	}
	record.ObservedAt = record.ObservedAt.UTC()
	return record, nil
}
