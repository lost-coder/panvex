import { z } from "zod";

import { id, timestamp } from "./common.ts";

/**
 * Runtime / agent schemas mirror internal/controlplane HTTP payloads.
 *
 * Principle: the schemas are DEFENSIVE, not prescriptive. If the backend
 * adds fields we don't know about, we let them through (Zod's default
 * object behaviour). We validate only the fields the UI reads, so that
 * backend additions are not release-blocking.
 */

const runtimeEventSchema = z.object({
  sequence: z.number(),
  timestamp_unix: z.number(),
  event_type: z.string(),
  context: z.string(),
});

const topByConnectionsSchema = z.object({
  username: z.string(),
  connections: z.number(),
});

const topByThroughputSchema = z.object({
  username: z.string(),
  throughput_bytes: z.number(),
});

const dcEntrySchema = z.object({
  dc: z.number(),
  available_endpoints: z.number(),
  available_pct: z.number(),
  required_writers: z.number(),
  alive_writers: z.number(),
  coverage_pct: z.number(),
  fresh_alive_writers: z.number(),
  fresh_coverage_pct: z.number(),
  rtt_ms: z.number(),
  load: z.number(),
});

const upstreamEntrySchema = z.object({
  upstream_id: z.number(),
  route_kind: z.string(),
  address: z.string(),
  healthy: z.boolean(),
  fails: z.number(),
  effective_latency_ms: z.number(),
  weight: z.number(),
  last_check_age_secs: z.number(),
  scopes: z.array(z.string()).optional(),
});

const systemLoadSchema = z.object({
  cpu_usage_pct: z.number(),
  memory_used_bytes: z.number(),
  memory_total_bytes: z.number(),
  memory_usage_pct: z.number(),
  disk_used_bytes: z.number(),
  disk_total_bytes: z.number(),
  disk_usage_pct: z.number(),
  load_1m: z.number(),
  load_5m: z.number(),
  load_15m: z.number(),
  net_bytes_sent: z.number(),
  net_bytes_recv: z.number(),
});

const meWritersSummarySchema = z.object({
  configured_endpoints: z.number(),
  available_endpoints: z.number(),
  coverage_pct: z.number(),
  fresh_alive_writers: z.number(),
  fresh_coverage_pct: z.number(),
  required_writers: z.number(),
  alive_writers: z.number(),
});

export const agentRuntimeSchema = z.object({
  accepting_new_connections: z.boolean(),
  me_runtime_ready: z.boolean(),
  me2dc_fallback_enabled: z.boolean(),
  use_middle_proxy: z.boolean(),
  startup_status: z.string(),
  startup_stage: z.string(),
  startup_progress_pct: z.number(),
  initialization_status: z.string(),
  degraded: z.boolean(),
  initialization_stage: z.string(),
  initialization_progress_pct: z.number(),
  transport_mode: z.string(),
  current_connections: z.number(),
  current_connections_me: z.number(),
  current_connections_direct: z.number(),
  active_users: z.number(),
  uptime_seconds: z.number(),
  connections_total: z.number(),
  connections_bad_total: z.number(),
  handshake_timeouts_total: z.number(),
  configured_users: z.number(),
  dc_coverage_pct: z.number(),
  healthy_upstreams: z.number(),
  total_upstreams: z.number(),
  reroute_active: z.boolean().optional(),
  route_mode: z.string().optional(),
  me2dc_fast_enabled: z.boolean().optional(),
  stale_cache_used: z.boolean().optional(),
  top_by_connections: z.array(topByConnectionsSchema).optional(),
  top_by_throughput: z.array(topByThroughputSchema).optional(),
  dcs: z.array(dcEntrySchema),
  upstreams: z.array(upstreamEntrySchema),
  lifecycle_state: z.string().optional(),
  updated_at: timestamp.optional(),
  recent_events: z.array(runtimeEventSchema),
  system_load: systemLoadSchema,
  me_writers_summary: meWritersSummarySchema.optional(),
});

const agentCertificateRecoverySchema = z.object({
  status: z.enum(["allowed", "expired", "used", "revoked"]),
  issued_at_unix: z.number(),
  expires_at_unix: z.number(),
  used_at_unix: z.number().optional(),
  revoked_at_unix: z.number().optional(),
});

/**
 * Single agent DTO — the list response from GET /api/agents is an array
 * of this shape.
 */
export const agentSchema = z.object({
  id,
  node_name: z.string(),
  fleet_group_id: z.string(),
  version: z.string(),
  read_only: z.boolean(),
  presence_state: z.string(),
  certificate_recovery: agentCertificateRecoverySchema.optional(),
  cert_issued_at: timestamp.optional(),
  cert_expires_at: timestamp.optional(),
  runtime: agentRuntimeSchema,
  last_seen_at: timestamp,
});

export const agentListSchema = z.array(agentSchema);

export type AgentParsed = z.infer<typeof agentSchema>;
