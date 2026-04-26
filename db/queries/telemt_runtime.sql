-- R-Q-03: telemt_runtime_current — minimal coverage so the table is
-- represented in db/queries. The full ~30-column shape stays inline
-- in internal/controlplane/storage/postgres/telemetry.go where the
-- bulk write helpers manage replace-by-agent semantics.

-- name: DeleteTelemetryRuntimeCurrent :exec
DELETE FROM telemt_runtime_current WHERE agent_id = $1;

-- name: DeleteTelemetryRuntimeDCsForAgent :exec
DELETE FROM telemt_runtime_dcs_current WHERE agent_id = $1;

-- name: DeleteTelemetryRuntimeUpstreamsForAgent :exec
DELETE FROM telemt_runtime_upstreams_current WHERE agent_id = $1;

-- name: DeleteTelemetryRuntimeEventsForAgent :exec
DELETE FROM telemt_runtime_events WHERE agent_id = $1;

-- name: DeleteTelemetryDiagnosticsCurrent :exec
DELETE FROM telemt_diagnostics_current WHERE agent_id = $1;

-- name: DeleteTelemetrySecurityInventoryCurrent :exec
DELETE FROM telemt_security_inventory_current WHERE agent_id = $1;

-- name: PruneTelemetryRuntimeEvents :execrows
DELETE FROM telemt_runtime_events WHERE observed_at < $1;
