import { z } from "zod";

import { agentSchema } from "./agent.ts";
import { fleetSchema } from "./dashboard.ts";

/**
 * R-Q-20: Zod schemas for the telemetry endpoints:
 * - GET /api/fleet
 * - GET /api/metrics
 * - GET /api/telemetry/servers
 * - GET /api/telemetry/servers/{id}
 * - POST /api/telemetry/servers/{id}/detail-boost
 * - POST /api/telemetry/servers/{id}/refresh-diagnostics
 * - GET /api/telemetry/servers/{id}/history/load
 * - GET /api/telemetry/servers/{id}/history/dc
 *
 * Schemas mirror the runtime types declared in shared/api/telemetry.ts
 * so the api<T>() ZodType<T> overload accepts them.
 */

export const fleetResponseSchema = fleetSchema;

export const metricSnapshotSchema = z.object({
  id: z.string(),
  agent_id: z.string(),
  instance_id: z.string(),
  captured_at: z.string(),
  values: z.record(z.string(), z.number()),
});

export const metricSnapshotListSchema = z.array(metricSnapshotSchema);

const telemetryFreshnessSchema = z.object({
  state: z.enum(["fresh", "stale", "unavailable", "disabled", "never_collected"]),
  observed_at_unix: z.number(),
});

export const telemetryDetailBoostSchema = z.object({
  active: z.boolean(),
  expires_at_unix: z.number(),
  remaining_seconds: z.number(),
});

export const telemetryDiagnosticsRefreshResponseSchema = z.object({
  job_id: z.string(),
  status: z.string(),
});

const telemetryServerSummarySchema = z.object({
  agent: agentSchema,
  severity: z.enum(["good", "ok", "warn", "critical", "bad"]),
  reason: z.string(),
  runtime_freshness: telemetryFreshnessSchema,
  detail_boost: telemetryDetailBoostSchema,
});

export const telemetryServersResponseSchema = z.object({
  servers: z.array(telemetryServerSummarySchema),
});

export const telemetryServerDetailResponseSchema = z.object({
  server: telemetryServerSummarySchema,
  initialization_watch: z.object({
    visible: z.boolean(),
    mode: z.enum(["active", "cooldown", "hidden"]),
    remaining_seconds: z.number(),
    completed_at_unix: z.number(),
    startup_status: z.string(),
    startup_stage: z.string(),
    startup_progress_pct: z.number(),
    initialization_status: z.string(),
    initialization_stage: z.string(),
    initialization_progress_pct: z.number(),
  }),
  diagnostics: z.object({
    state: z.string(),
    state_reason: z.string(),
    system_info: z.record(z.string(), z.unknown()),
    effective_limits: z.record(z.string(), z.unknown()),
    security_posture: z.record(z.string(), z.unknown()),
    minimal_all: z.record(z.string(), z.unknown()),
    me_pool: z.record(z.string(), z.unknown()),
    dcs_detail: z.record(z.string(), z.unknown()),
  }),
  security_inventory: z.object({
    state: z.string(),
    state_reason: z.string(),
    enabled: z.boolean(),
    entries_total: z.number(),
    entries: z.array(z.string()),
  }),
});

const serverLoadPointSchema = z.object({
  AgentID: z.string(),
  CapturedAt: z.string(),
  CPUPctAvg: z.number(),
  CPUPctMax: z.number(),
  MemPctAvg: z.number(),
  MemPctMax: z.number(),
  DiskPctAvg: z.number(),
  DiskPctMax: z.number(),
  Load1M: z.number(),
  ConnectionsAvg: z.number(),
  ConnectionsMax: z.number(),
  ActiveUsersAvg: z.number(),
  ActiveUsersMax: z.number(),
  DCCoverageMinPct: z.number(),
  HealthyUpstreams: z.number(),
  TotalUpstreams: z.number(),
  NetBytesSent: z.number(),
  NetBytesRecv: z.number(),
  SampleCount: z.number(),
});

export const serverLoadHistoryResponseSchema = z.object({
  points: z.array(serverLoadPointSchema),
  resolution: z.enum(["raw", "hourly"]),
});

const dcHealthPointSchema = z.object({
  AgentID: z.string(),
  CapturedAt: z.string(),
  DC: z.number(),
  CoveragePctAvg: z.number(),
  CoveragePctMin: z.number(),
  RTTMsAvg: z.number(),
  RTTMsMax: z.number(),
  AliveWritersMin: z.number(),
  RequiredWriters: z.number(),
  SampleCount: z.number(),
});

export const dcHealthHistoryResponseSchema = z.object({
  points: z.array(dcHealthPointSchema),
});

export type TelemetryDetailBoostParsed = z.infer<typeof telemetryDetailBoostSchema>;
export type TelemetryServersResponseParsed = z.infer<typeof telemetryServersResponseSchema>;
export type TelemetryServerDetailResponseParsed = z.infer<typeof telemetryServerDetailResponseSchema>;
export type ServerLoadHistoryResponseParsed = z.infer<typeof serverLoadHistoryResponseSchema>;
export type DCHealthHistoryResponseParsed = z.infer<typeof dcHealthHistoryResponseSchema>;
