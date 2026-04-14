package sqlite

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
			agent_id, observed_at_unix, state, state_reason, read_only, accepting_new_connections,
			me_runtime_ready, me2dc_fallback_enabled, use_middle_proxy, startup_status, startup_stage,
			startup_progress_pct, initialization_status, degraded, initialization_stage, initialization_progress_pct,
			transport_mode, current_connections, current_connections_me, current_connections_direct, active_users,
			uptime_seconds, connections_total, connections_bad_total, handshake_timeouts_total, configured_users,
			dc_coverage_pct, healthy_upstreams, total_upstreams
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(agent_id) DO UPDATE SET
			observed_at_unix = excluded.observed_at_unix,
			state = excluded.state,
			state_reason = excluded.state_reason,
			read_only = excluded.read_only,
			accepting_new_connections = excluded.accepting_new_connections,
			me_runtime_ready = excluded.me_runtime_ready,
			me2dc_fallback_enabled = excluded.me2dc_fallback_enabled,
			use_middle_proxy = excluded.use_middle_proxy,
			startup_status = excluded.startup_status,
			startup_stage = excluded.startup_stage,
			startup_progress_pct = excluded.startup_progress_pct,
			initialization_status = excluded.initialization_status,
			degraded = excluded.degraded,
			initialization_stage = excluded.initialization_stage,
			initialization_progress_pct = excluded.initialization_progress_pct,
			transport_mode = excluded.transport_mode,
			current_connections = excluded.current_connections,
			current_connections_me = excluded.current_connections_me,
			current_connections_direct = excluded.current_connections_direct,
			active_users = excluded.active_users,
			uptime_seconds = excluded.uptime_seconds,
			connections_total = excluded.connections_total,
			connections_bad_total = excluded.connections_bad_total,
			handshake_timeouts_total = excluded.handshake_timeouts_total,
			configured_users = excluded.configured_users,
			dc_coverage_pct = excluded.dc_coverage_pct,
			healthy_upstreams = excluded.healthy_upstreams,
			total_upstreams = excluded.total_upstreams
	`,
		record.AgentID, toUnix(record.ObservedAt), record.State, record.StateReason, boolToInt(record.ReadOnly),
		boolToInt(record.AcceptingNewConnections), boolToInt(record.MERuntimeReady), boolToInt(record.ME2DCFallbackEnabled),
		boolToInt(record.UseMiddleProxy), record.StartupStatus, record.StartupStage, record.StartupProgressPct,
		record.InitializationStatus, boolToInt(record.Degraded), record.InitializationStage, record.InitializationProgressPct,
		record.TransportMode, record.CurrentConnections, record.CurrentConnectionsME, record.CurrentConnectionsDirect,
		record.ActiveUsers, record.UptimeSeconds, int64(record.ConnectionsTotal), int64(record.ConnectionsBadTotal),
		int64(record.HandshakeTimeoutsTotal), record.ConfiguredUsers, record.DCCoveragePct, record.HealthyUpstreams, record.TotalUpstreams,
	)
	return err
}

func (s *Store) GetTelemetryRuntimeCurrent(ctx context.Context, agentID string) (storage.TelemetryRuntimeCurrentRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT agent_id, observed_at_unix, state, state_reason, read_only, accepting_new_connections,
		       me_runtime_ready, me2dc_fallback_enabled, use_middle_proxy, startup_status, startup_stage,
		       startup_progress_pct, initialization_status, degraded, initialization_stage, initialization_progress_pct,
		       transport_mode, current_connections, current_connections_me, current_connections_direct, active_users,
		       uptime_seconds, connections_total, connections_bad_total, handshake_timeouts_total, configured_users,
		       dc_coverage_pct, healthy_upstreams, total_upstreams
		FROM telemt_runtime_current
		WHERE agent_id = ?
	`, agentID)

	var record storage.TelemetryRuntimeCurrentRecord
	var observedAt int64
	var readOnly int
	var accepting int
	var meRuntimeReady int
	var meFallback int
	var useMiddleProxy int
	var degraded int
	var connectionsTotal int64
	var connectionsBadTotal int64
	var handshakeTimeoutsTotal int64
	if err := row.Scan(
		&record.AgentID, &observedAt, &record.State, &record.StateReason, &readOnly, &accepting,
		&meRuntimeReady, &meFallback, &useMiddleProxy, &record.StartupStatus, &record.StartupStage,
		&record.StartupProgressPct, &record.InitializationStatus, &degraded, &record.InitializationStage, &record.InitializationProgressPct,
		&record.TransportMode, &record.CurrentConnections, &record.CurrentConnectionsME, &record.CurrentConnectionsDirect, &record.ActiveUsers,
		&record.UptimeSeconds, &connectionsTotal, &connectionsBadTotal, &handshakeTimeoutsTotal, &record.ConfiguredUsers,
		&record.DCCoveragePct, &record.HealthyUpstreams, &record.TotalUpstreams,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.TelemetryRuntimeCurrentRecord{}, storage.ErrNotFound
		}
		return storage.TelemetryRuntimeCurrentRecord{}, err
	}

	record.ObservedAt = fromUnix(observedAt)
	record.ReadOnly = intToBool(readOnly)
	record.AcceptingNewConnections = intToBool(accepting)
	record.MERuntimeReady = intToBool(meRuntimeReady)
	record.ME2DCFallbackEnabled = intToBool(meFallback)
	record.UseMiddleProxy = intToBool(useMiddleProxy)
	record.Degraded = intToBool(degraded)
	record.ConnectionsTotal = uint64(connectionsTotal)
	record.ConnectionsBadTotal = uint64(connectionsBadTotal)
	record.HandshakeTimeoutsTotal = uint64(handshakeTimeoutsTotal)
	return record, nil
}

func (s *Store) ListTelemetryRuntimeCurrent(ctx context.Context) ([]storage.TelemetryRuntimeCurrentRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT agent_id, observed_at_unix, state, state_reason, read_only, accepting_new_connections,
		       me_runtime_ready, me2dc_fallback_enabled, use_middle_proxy, startup_status, startup_stage,
		       startup_progress_pct, initialization_status, degraded, initialization_stage, initialization_progress_pct,
		       transport_mode, current_connections, current_connections_me, current_connections_direct, active_users,
		       uptime_seconds, connections_total, connections_bad_total, handshake_timeouts_total, configured_users,
		       dc_coverage_pct, healthy_upstreams, total_upstreams
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
		var readOnly int
		var accepting int
		var meRuntimeReady int
		var meFallback int
		var useMiddleProxy int
		var degraded int
		var connectionsTotal int64
		var connectionsBadTotal int64
		var handshakeTimeoutsTotal int64
		if err := rows.Scan(
			&record.AgentID, &observedAt, &record.State, &record.StateReason, &readOnly, &accepting,
			&meRuntimeReady, &meFallback, &useMiddleProxy, &record.StartupStatus, &record.StartupStage,
			&record.StartupProgressPct, &record.InitializationStatus, &degraded, &record.InitializationStage, &record.InitializationProgressPct,
			&record.TransportMode, &record.CurrentConnections, &record.CurrentConnectionsME, &record.CurrentConnectionsDirect, &record.ActiveUsers,
			&record.UptimeSeconds, &connectionsTotal, &connectionsBadTotal, &handshakeTimeoutsTotal, &record.ConfiguredUsers,
			&record.DCCoveragePct, &record.HealthyUpstreams, &record.TotalUpstreams,
		); err != nil {
			return nil, err
		}
		record.ObservedAt = fromUnix(observedAt)
		record.ReadOnly = intToBool(readOnly)
		record.AcceptingNewConnections = intToBool(accepting)
		record.MERuntimeReady = intToBool(meRuntimeReady)
		record.ME2DCFallbackEnabled = intToBool(meFallback)
		record.UseMiddleProxy = intToBool(useMiddleProxy)
		record.Degraded = intToBool(degraded)
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

func (s *Store) ReplaceTelemetryRuntimeUpstreams(ctx context.Context, agentID string, records []storage.TelemetryRuntimeUpstreamRecord) error {
	tx, err := s.db.BeginTx(ctx, nil)
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

func (s *Store) AppendTelemetryRuntimeEvents(ctx context.Context, agentID string, records []storage.TelemetryRuntimeEventRecord) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, record := range records {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO telemt_runtime_events (
				agent_id, sequence, observed_at_unix, timestamp_unix, event_type, context, severity
			)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(agent_id, sequence) DO UPDATE SET
				observed_at_unix = excluded.observed_at_unix,
				timestamp_unix = excluded.timestamp_unix,
				event_type = excluded.event_type,
				context = excluded.context,
				severity = excluded.severity
		`, agentID, record.Sequence, toUnix(record.ObservedAt), toUnix(record.Timestamp), record.EventType, record.Context, record.Severity); err != nil {
			return err
		}
	}

	return tx.Commit()
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

func (s *Store) PutTelemetryDetailBoost(ctx context.Context, record storage.TelemetryDetailBoostRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO telemt_detail_boosts (agent_id, expires_at_unix, updated_at_unix)
		VALUES (?, ?, ?)
		ON CONFLICT(agent_id) DO UPDATE SET
			expires_at_unix = excluded.expires_at_unix,
			updated_at_unix = excluded.updated_at_unix
	`, record.AgentID, toUnix(record.ExpiresAt), toUnix(record.UpdatedAt))
	return err
}

func (s *Store) ListTelemetryDetailBoosts(ctx context.Context) ([]storage.TelemetryDetailBoostRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT agent_id, expires_at_unix, updated_at_unix
		FROM telemt_detail_boosts
		ORDER BY expires_at_unix, agent_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.TelemetryDetailBoostRecord, 0)
	for rows.Next() {
		var record storage.TelemetryDetailBoostRecord
		var expiresAt int64
		var updatedAt int64
		if err := rows.Scan(&record.AgentID, &expiresAt, &updatedAt); err != nil {
			return nil, err
		}
		record.ExpiresAt = fromUnix(expiresAt)
		record.UpdatedAt = fromUnix(updatedAt)
		result = append(result, record)
	}

	return result, rows.Err()
}

func (s *Store) DeleteTelemetryDetailBoost(ctx context.Context, agentID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM telemt_detail_boosts WHERE agent_id = ?`, agentID)
	return err
}
