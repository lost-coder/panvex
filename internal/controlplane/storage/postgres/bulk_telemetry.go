// Package postgres telemetry bulk helpers (P6-6.1a, аудит #10).
//
// Multi-row/one-tx variants of the per-agent telemetry writers, mirroring
// sqlite/bulk_telemetry.go. Dedup helpers are duplicated here because the
// two backend packages share no internal path (same as dedupAgents).
package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// dedupRuntimeCurrent keeps only the LAST occurrence per AgentID,
// preserving first-occurrence order. Postgres cannot upsert the same
// conflict key twice in one statement (SQLSTATE 21000).
func dedupRuntimeCurrent(records []storage.TelemetryRuntimeCurrentRecord) []storage.TelemetryRuntimeCurrentRecord {
	last := make(map[string]int, len(records))
	for i, r := range records {
		last[r.AgentID] = i
	}
	if len(last) == len(records) {
		return records
	}
	out := make([]storage.TelemetryRuntimeCurrentRecord, 0, len(last))
	for i, r := range records {
		if last[r.AgentID] == i {
			out = append(out, r)
		}
	}
	return out
}

func (s *Store) PutTelemetryRuntimeCurrentBulk(ctx context.Context, records []storage.TelemetryRuntimeCurrentRecord) error {
	if len(records) == 0 {
		return nil
	}
	records = dedupRuntimeCurrent(records)
	const cols = 3
	return s.execInTx(ctx, func(exec dbExecutor) error {
		return runBulkChunks(ctx, exec, len(records), cols,
			func(ph string) string {
				return fmt.Sprintf(`
					INSERT INTO telemt_runtime_current (agent_id, observed_at, runtime_json) VALUES %s
					ON CONFLICT (agent_id) DO UPDATE
					SET observed_at = EXCLUDED.observed_at,
					    runtime_json = EXCLUDED.runtime_json`, ph)
			},
			func(start, end int) ([]any, error) {
				args := make([]any, 0, (end-start)*cols)
				for _, r := range records[start:end] {
					args = append(args, r.AgentID, r.ObservedAt.UTC(), r.RuntimeJSON)
				}
				return args, nil
			},
		)
	})
}

// deleteAgentRowsChunked deletes all rows of `table` for the given agent IDs,
// chunked with numbered placeholders so the IN-list never exceeds the bind cap.
func deleteAgentRowsChunked(ctx context.Context, exec dbExecutor, table string, agentIDs []string) error {
	for start := 0; start < len(agentIDs); start += bulkChunkSize {
		s, e := chunkBounds(start, len(agentIDs))
		chunk := agentIDs[s:e]
		ph := make([]string, len(chunk))
		args := make([]any, len(chunk))
		for i, id := range chunk {
			ph[i] = fmt.Sprintf("$%d", i+1)
			args[i] = id
		}
		query := fmt.Sprintf(`DELETE FROM %s WHERE agent_id IN (%s)`, table, strings.Join(ph, ",")) //nolint:gosec // table is a compile-time constant
		if _, err := exec.ExecContext(ctx, query, args...); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ReplaceTelemetryRuntimeDCsBulk(ctx context.Context, byAgent map[string][]storage.TelemetryRuntimeDCRecord) error {
	if len(byAgent) == 0 {
		return nil
	}
	agentIDs := make([]string, 0, len(byAgent))
	var rows []storage.TelemetryRuntimeDCRecord
	for agentID, records := range byAgent {
		agentIDs = append(agentIDs, agentID)
		rows = append(rows, records...)
	}
	const cols = 10
	return s.execInTx(ctx, func(exec dbExecutor) error {
		if err := deleteAgentRowsChunked(ctx, exec, "telemt_runtime_dcs_current", agentIDs); err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		return runBulkChunks(ctx, exec, len(rows), cols,
			func(ph string) string {
				return `INSERT INTO telemt_runtime_dcs_current (
						agent_id, dc, observed_at, available_endpoints, available_pct,
						required_writers, alive_writers, coverage_pct, rtt_ms, load
					) VALUES ` + ph
			},
			func(start, end int) ([]any, error) {
				args := make([]any, 0, (end-start)*cols)
				for _, r := range rows[start:end] {
					args = append(args,
						r.AgentID, r.DC, r.ObservedAt.UTC(), r.AvailableEndpoints, r.AvailablePct,
						r.RequiredWriters, r.AliveWriters, r.CoveragePct, r.RTTMs, r.Load)
				}
				return args, nil
			},
		)
	})
}

func (s *Store) ReplaceTelemetryRuntimeUpstreamsBulk(ctx context.Context, byAgent map[string][]storage.TelemetryRuntimeUpstreamRecord) error {
	if len(byAgent) == 0 {
		return nil
	}
	agentIDs := make([]string, 0, len(byAgent))
	var rows []storage.TelemetryRuntimeUpstreamRecord
	for agentID, records := range byAgent {
		agentIDs = append(agentIDs, agentID)
		rows = append(rows, records...)
	}
	const cols = 8
	return s.execInTx(ctx, func(exec dbExecutor) error {
		if err := deleteAgentRowsChunked(ctx, exec, "telemt_runtime_upstreams_current", agentIDs); err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		return runBulkChunks(ctx, exec, len(rows), cols,
			func(ph string) string {
				return `INSERT INTO telemt_runtime_upstreams_current (
						agent_id, upstream_id, observed_at, route_kind, address, healthy, fails, effective_latency_ms
					) VALUES ` + ph
			},
			func(start, end int) ([]any, error) {
				args := make([]any, 0, (end-start)*cols)
				for _, r := range rows[start:end] {
					args = append(args,
						r.AgentID, r.UpstreamID, r.ObservedAt.UTC(), r.RouteKind, r.Address,
						r.Healthy, r.Fails, r.EffectiveLatencyMs)
				}
				return args, nil
			},
		)
	})
}

// dedupRuntimeEvents keeps the last occurrence per (agent_id, sequence).
func dedupRuntimeEvents(records []storage.TelemetryRuntimeEventRecord) []storage.TelemetryRuntimeEventRecord {
	type key struct {
		agentID  string
		sequence int64
	}
	last := make(map[key]int, len(records))
	for i, r := range records {
		last[key{r.AgentID, r.Sequence}] = i
	}
	if len(last) == len(records) {
		return records
	}
	out := make([]storage.TelemetryRuntimeEventRecord, 0, len(last))
	for i, r := range records {
		if last[key{r.AgentID, r.Sequence}] == i {
			out = append(out, r)
		}
	}
	return out
}

func (s *Store) AppendTelemetryRuntimeEventsBulk(ctx context.Context, records []storage.TelemetryRuntimeEventRecord) error {
	if len(records) == 0 {
		return nil
	}
	records = dedupRuntimeEvents(records)
	const cols = 7
	return s.execInTx(ctx, func(exec dbExecutor) error {
		return runBulkChunks(ctx, exec, len(records), cols,
			func(ph string) string {
				return `INSERT INTO telemt_runtime_events (
						agent_id, sequence, observed_at, timestamp_at, event_type, context, severity
					) VALUES ` + ph +
					` ON CONFLICT (agent_id, sequence) DO UPDATE
					SET observed_at = EXCLUDED.observed_at,
					    timestamp_at = EXCLUDED.timestamp_at,
					    event_type = EXCLUDED.event_type,
					    context = EXCLUDED.context,
					    severity = EXCLUDED.severity`
			},
			func(start, end int) ([]any, error) {
				args := make([]any, 0, (end-start)*cols)
				for _, r := range records[start:end] {
					args = append(args,
						r.AgentID, r.Sequence, r.ObservedAt.UTC(), r.Timestamp.UTC(),
						r.EventType, r.Context, r.Severity)
				}
				return args, nil
			},
		)
	})
}

// dedupDiagnostics keeps the last occurrence per AgentID.
func dedupDiagnostics(records []storage.TelemetryDiagnosticsCurrentRecord) []storage.TelemetryDiagnosticsCurrentRecord {
	last := make(map[string]int, len(records))
	for i, r := range records {
		last[r.AgentID] = i
	}
	if len(last) == len(records) {
		return records
	}
	out := make([]storage.TelemetryDiagnosticsCurrentRecord, 0, len(last))
	for i, r := range records {
		if last[r.AgentID] == i {
			out = append(out, r)
		}
	}
	return out
}

func (s *Store) PutTelemetryDiagnosticsCurrentBulk(ctx context.Context, records []storage.TelemetryDiagnosticsCurrentRecord) error {
	if len(records) == 0 {
		return nil
	}
	records = dedupDiagnostics(records)
	const cols = 10
	return s.execInTx(ctx, func(exec dbExecutor) error {
		return runBulkChunks(ctx, exec, len(records), cols,
			func(ph string) string {
				return `INSERT INTO telemt_diagnostics_current (
						agent_id, observed_at, state, state_reason, system_info_json, effective_limits_json,
						security_posture_json, minimal_all_json, me_pool_json, dcs_json
					) VALUES ` + ph +
					` ON CONFLICT (agent_id) DO UPDATE
					SET observed_at = EXCLUDED.observed_at,
					    state = EXCLUDED.state,
					    state_reason = EXCLUDED.state_reason,
					    system_info_json = EXCLUDED.system_info_json,
					    effective_limits_json = EXCLUDED.effective_limits_json,
					    security_posture_json = EXCLUDED.security_posture_json,
					    minimal_all_json = EXCLUDED.minimal_all_json,
					    me_pool_json = EXCLUDED.me_pool_json,
					    dcs_json = EXCLUDED.dcs_json`
			},
			func(start, end int) ([]any, error) {
				args := make([]any, 0, (end-start)*cols)
				for _, r := range records[start:end] {
					args = append(args,
						r.AgentID, r.ObservedAt.UTC(), r.State, r.StateReason, r.SystemInfoJSON, r.EffectiveLimitsJSON,
						r.SecurityPostureJSON, r.MinimalAllJSON, r.MEPoolJSON, r.DcsJSON)
				}
				return args, nil
			},
		)
	})
}

// dedupSecurityInventory keeps the last occurrence per AgentID.
func dedupSecurityInventory(records []storage.TelemetrySecurityInventoryCurrentRecord) []storage.TelemetrySecurityInventoryCurrentRecord {
	last := make(map[string]int, len(records))
	for i, r := range records {
		last[r.AgentID] = i
	}
	if len(last) == len(records) {
		return records
	}
	out := make([]storage.TelemetrySecurityInventoryCurrentRecord, 0, len(last))
	for i, r := range records {
		if last[r.AgentID] == i {
			out = append(out, r)
		}
	}
	return out
}

func (s *Store) PutTelemetrySecurityInventoryCurrentBulk(ctx context.Context, records []storage.TelemetrySecurityInventoryCurrentRecord) error {
	if len(records) == 0 {
		return nil
	}
	records = dedupSecurityInventory(records)
	const cols = 7
	return s.execInTx(ctx, func(exec dbExecutor) error {
		return runBulkChunks(ctx, exec, len(records), cols,
			func(ph string) string {
				return `INSERT INTO telemt_security_inventory_current (
						agent_id, observed_at, state, state_reason, enabled, entries_total, entries_json
					) VALUES ` + ph +
					` ON CONFLICT (agent_id) DO UPDATE
					SET observed_at = EXCLUDED.observed_at,
					    state = EXCLUDED.state,
					    state_reason = EXCLUDED.state_reason,
					    enabled = EXCLUDED.enabled,
					    entries_total = EXCLUDED.entries_total,
					    entries_json = EXCLUDED.entries_json`
			},
			func(start, end int) ([]any, error) {
				args := make([]any, 0, (end-start)*cols)
				for _, r := range records[start:end] {
					args = append(args,
						r.AgentID, r.ObservedAt.UTC(), r.State, r.StateReason,
						r.Enabled, r.EntriesTotal, r.EntriesJSON)
				}
				return args, nil
			},
		)
	})
}
