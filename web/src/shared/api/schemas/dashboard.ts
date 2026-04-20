import { z } from "zod";

import { agentRuntimeSchema, agentSchema } from "./agent.ts";

/**
 * Dashboard schemas cover the two "landing page" endpoints:
 * - GET /api/control-room  (onboarding + fleet summary + recent activity)
 * - GET /api/telemetry/dashboard (per-agent health cards + attention list)
 *
 * These shapes are intentionally narrow: every field exercised by the
 * React dashboard containers is validated. The Record<string, unknown>
 * fields on detail endpoints are intentionally left untyped because they
 * carry backend-specific diagnostics we surface verbatim.
 */

export const fleetSchema = z.object({
  total_agents: z.number(),
  online_agents: z.number(),
  degraded_agents: z.number(),
  offline_agents: z.number(),
  total_instances: z.number(),
  metric_snapshots: z.number(),
  live_connections: z.number(),
  accepting_new_connections_agents: z.number(),
  middle_proxy_agents: z.number(),
  dc_issue_agents: z.number(),
});

const runtimeEventSchema = z.object({
  sequence: z.number(),
  timestamp_unix: z.number(),
  event_type: z.string(),
  context: z.string(),
});

const auditEventSchema = z.object({
  id: z.string(),
  actor_id: z.string(),
  action: z.string(),
  target_id: z.string(),
  created_at: z.string(),
  details: z.record(z.string(), z.unknown()),
});

export const controlRoomSchema = z.object({
  onboarding: z.object({
    needs_first_server: z.boolean(),
    setup_complete: z.boolean(),
    suggested_fleet_group_id: z.string(),
  }),
  fleet: fleetSchema,
  jobs: z.object({
    total: z.number(),
    queued: z.number(),
    running: z.number(),
    failed: z.number(),
  }),
  recent_activity: z.array(auditEventSchema),
  recent_runtime_events: z.array(runtimeEventSchema),
});

const telemetryFreshnessSchema = z.object({
  state: z.enum(["fresh", "stale", "unavailable", "disabled", "never_collected"]),
  observed_at_unix: z.number(),
});

const telemetryDetailBoostSchema = z.object({
  active: z.boolean(),
  expires_at_unix: z.number(),
  remaining_seconds: z.number(),
});

const telemetryServerSummarySchema = z.object({
  agent: agentSchema,
  severity: z.enum(["good", "warn", "bad"]),
  reason: z.string(),
  runtime_freshness: telemetryFreshnessSchema,
  detail_boost: telemetryDetailBoostSchema,
});

const telemetryAttentionItemSchema = z.object({
  agent_id: z.string(),
  node_name: z.string(),
  fleet_group_id: z.string(),
  severity: z.enum(["good", "warn", "bad"]),
  reason: z.string(),
  presence_state: z.string(),
  runtime: agentRuntimeSchema,
  runtime_freshness: telemetryFreshnessSchema,
  detail_boost: telemetryDetailBoostSchema,
});

/**
 * Aggregated telemetry payload for the dashboard: fleet totals + per-agent
 * attention rows + server cards. This is the closest shape we have to a
 * "dashboard" resource; the task's `dashboardSchema` maps to it.
 */
export const dashboardSchema = z.object({
  fleet: fleetSchema,
  attention: z.array(telemetryAttentionItemSchema),
  server_cards: z.array(telemetryServerSummarySchema),
  runtime_distribution: z.record(z.string(), z.number()),
  recent_runtime_events: z.array(runtimeEventSchema),
});

export type DashboardParsed = z.infer<typeof dashboardSchema>;
export type ControlRoomParsed = z.infer<typeof controlRoomSchema>;
