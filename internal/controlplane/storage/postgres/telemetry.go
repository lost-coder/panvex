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
		INSERT INTO telemt_runtime_current (
			agent_id, observed_at, state, state_reason, read_only, accepting_new_connections,
			me_runtime_ready, me2dc_fallback_enabled, use_middle_proxy, startup_status, startup_stage,
			startup_progress_pct, initialization_status, degraded, initialization_stage, initialization_progress_pct,
			transport_mode, current_connections, current_connections_me, current_connections_direct, active_users,
			uptime_seconds, connections_total, connections_bad_total, handshake_timeouts_total, configured_users,
			dc_coverage_pct, healthy_upstreams, total_upstreams
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29)
		ON CONFLICT (agent_id) DO UPDATE
		SET observed_at = EXCLUDED.observed_at,
		    state = EXCLUDED.state,
		    state_reason = EXCLUDED.state_reason,
		    read_only = EXCLUDED.read_only,
		    accepting_new_connections = EXCLUDED.accepting_new_connections,
		    me_runtime_ready = EXCLUDED.me_runtime_ready,
		    me2dc_fallback_enabled = EXCLUDED.me2dc_fallback_enabled,
		    use_middle_proxy = EXCLUDED.use_middle_proxy,
		    startup_status = EXCLUDED.startup_status,
		    startup_stage = EXCLUDED.startup_stage,
		    startup_progress_pct = EXCLUDED.startup_progress_pct,
		    initialization_status = EXCLUDED.initialization_status,
		    degraded = EXCLUDED.degraded,
		    initialization_stage = EXCLUDED.initialization_stage,
		    initialization_progress_pct = EXCLUDED.initialization_progress_pct,
		    transport_mode = EXCLUDED.transport_mode,
		    current_connections = EXCLUDED.current_connections,
		    current_connections_me = EXCLUDED.current_connections_me,
		    current_connections_direct = EXCLUDED.current_connections_direct,
		    active_users = EXCLUDED.active_users,
		    uptime_seconds = EXCLUDED.uptime_seconds,
		    connections_total = EXCLUDED.connections_total,
		    connections_bad_total = EXCLUDED.connections_bad_total,
		    handshake_timeouts_total = EXCLUDED.handshake_timeouts_total,
		    configured_users = EXCLUDED.configured_users,
		    dc_coverage_pct = EXCLUDED.dc_coverage_pct,
		    healthy_upstreams = EXCLUDED.healthy_upstreams,
		    total_upstreams = EXCLUDED.total_upstreams
	`,
		record.AgentID, record.ObservedAt.UTC(), record.State, record.StateReason, record.ReadOnly, record.AcceptingNewConnections,
		record.MERuntimeReady, record.ME2DCFallbackEnabled, record.UseMiddleProxy, record.StartupStatus, record.StartupStage,
		record.StartupProgressPct, record.InitializationStatus, record.Degraded, record.InitializationStage, record.InitializationProgressPct,
		record.TransportMode, record.CurrentConnections, record.CurrentConnectionsME, record.CurrentConnectionsDirect, record.ActiveUsers,
		record.UptimeSeconds, int64(record.ConnectionsTotal), int64(record.ConnectionsBadTotal), int64(record.HandshakeTimeoutsTotal),
		record.ConfiguredUsers, record.DCCoveragePct, record.HealthyUpstreams, record.TotalUpstreams,
	)
	return err
}

func (s *Store) GetTelemetryRuntimeCurrent(ctx context.Context, agentID string) (storage.TelemetryRuntimeCurrentRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT agent_id, observed_at, state, state_reason, read_only, accepting_new_connections,
		       me_runtime_ready, me2dc_fallback_enabled, use_middle_proxy, startup_status, startup_stage,
		       startup_progress_pct, initialization_status, degraded, initialization_stage, initialization_progress_pct,
		       transport_mode, current_connections, current_connections_me, current_connections_direct, active_users,
		       uptime_seconds, connections_total, connections_bad_total, handshake_timeouts_total, configured_users,
		       dc_coverage_pct, healthy_upstreams, total_upstreams
		FROM telemt_runtime_current
		WHERE agent_id = $1
	`, agentID)

	var record storage.TelemetryRuntimeCurrentRecord
	var connectionsTotal int64
	var connectionsBadTotal int64
	var handshakeTimeoutsTotal int64
	if err := row.Scan(
		&record.AgentID, &record.ObservedAt, &record.State, &record.StateReason, &record.ReadOnly, &record.AcceptingNewConnections,
		&record.MERuntimeReady, &record.ME2DCFallbackEnabled, &record.UseMiddleProxy, &record.StartupStatus, &record.StartupStage,
		&record.StartupProgressPct, &record.InitializationStatus, &record.Degraded, &record.InitializationStage, &record.InitializationProgressPct,
		&record.TransportMode, &record.CurrentConnections, &record.CurrentConnectionsME, &record.CurrentConnectionsDirect, &record.ActiveUsers,
		&record.UptimeSeconds, &connectionsTotal, &connectionsBadTotal, &handshakeTimeoutsTotal, &record.ConfiguredUsers,
		&record.DCCoveragePct, &record.HealthyUpstreams, &record.TotalUpstreams,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.TelemetryRuntimeCurrentRecord{}, storage.ErrNotFound
		}
		return storage.TelemetryRuntimeCurrentRecord{}, err
	}

	record.ObservedAt = record.ObservedAt.UTC()
	record.ConnectionsTotal = uint64(connectionsTotal)
	record.ConnectionsBadTotal = uint64(connectionsBadTotal)
	record.HandshakeTimeoutsTotal = uint64(handshakeTimeoutsTotal)
	return record, nil
}

func (s *Store) ListTelemetryRuntimeCurrent(ctx context.Context) ([]storage.TelemetryRuntimeCurrentRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT agent_id, observed_at, state, state_reason, read_only, accepting_new_connections,
		       me_runtime_ready, me2dc_fallback_enabled, use_middle_proxy, startup_status, startup_stage,
		       startup_progress_pct, initialization_status, degraded, initialization_stage, initialization_progress_pct,
		       transport_mode, current_connections, current_connections_me, current_connections_direct, active_users,
		       uptime_seconds, connections_total, connections_bad_total, handshake_timeouts_total, configured_users,
		       dc_coverage_pct, healthy_upstreams, total_upstreams
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
		var connectionsTotal int64
		var connectionsBadTotal int64
		var handshakeTimeoutsTotal int64
		if err := rows.Scan(
			&record.AgentID, &record.ObservedAt, &record.State, &record.StateReason, &record.ReadOnly, &record.AcceptingNewConnections,
			&record.MERuntimeReady, &record.ME2DCFallbackEnabled, &record.UseMiddleProxy, &record.StartupStatus, &record.StartupStage,
			&record.StartupProgressPct, &record.InitializationStatus, &record.Degraded, &record.InitializationStage, &record.InitializationProgressPct,
			&record.TransportMode, &record.CurrentConnections, &record.CurrentConnectionsME, &record.CurrentConnectionsDirect, &record.ActiveUsers,
			&record.UptimeSeconds, &connectionsTotal, &connectionsBadTotal, &handshakeTimeoutsTotal, &record.ConfiguredUsers,
			&record.DCCoveragePct, &record.HealthyUpstreams, &record.TotalUpstreams,
		); err != nil {
			return nil, err
		}
		record.ObservedAt = record.ObservedAt.UTC()
		record.ConnectionsTotal = uint64(connectionsTotal)
		record.ConnectionsBadTotal = uint64(connectionsBadTotal)
		record.HandshakeTimeoutsTotal = uint64(handshakeTimeoutsTotal)
		result = append(result, record)
	}

	return result, rows.Err()
}

func (s *Store) ReplaceTelemetryRuntimeDCs(ctx context.Context, agentID string, records []storage.TelemetryRuntimeDCRecord) error {
	tx, err := s.db.BeginTx(ctx, nil)
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

func (s *Store) ReplaceTelemetryRuntimeUpstreams(ctx context.Context, agentID string, records []storage.TelemetryRuntimeUpstreamRecord) error {
	tx, err := s.db.BeginTx(ctx, nil)
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

func (s *Store) AppendTelemetryRuntimeEvents(ctx context.Context, agentID string, records []storage.TelemetryRuntimeEventRecord) error {
	tx, err := s.db.BeginTx(ctx, nil)
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

func (s *Store) PutTelemetryDetailBoost(ctx context.Context, record storage.TelemetryDetailBoostRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO telemt_detail_boosts (agent_id, expires_at, updated_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (agent_id) DO UPDATE
		SET expires_at = EXCLUDED.expires_at,
		    updated_at = EXCLUDED.updated_at
	`, record.AgentID, record.ExpiresAt.UTC(), record.UpdatedAt.UTC())
	return err
}

func (s *Store) ListTelemetryDetailBoosts(ctx context.Context) ([]storage.TelemetryDetailBoostRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT agent_id, expires_at, updated_at
		FROM telemt_detail_boosts
		ORDER BY expires_at, agent_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.TelemetryDetailBoostRecord, 0)
	for rows.Next() {
		var record storage.TelemetryDetailBoostRecord
		if err := rows.Scan(&record.AgentID, &record.ExpiresAt, &record.UpdatedAt); err != nil {
			return nil, err
		}
		record.ExpiresAt = record.ExpiresAt.UTC()
		record.UpdatedAt = record.UpdatedAt.UTC()
		result = append(result, record)
	}

	return result, rows.Err()
}

func (s *Store) DeleteTelemetryDetailBoost(ctx context.Context, agentID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM telemt_detail_boosts WHERE agent_id = $1`, agentID)
	return err
}
