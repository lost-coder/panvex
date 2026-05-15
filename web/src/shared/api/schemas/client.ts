import { z } from "zod";

import { id } from "./common.ts";

const clientDeploymentSchema = z.object({
  agent_id: z.string(),
  desired_operation: z.string(),
  status: z.string(),
  last_error: z.string(),
  connection_links: z.array(z.string()),
  last_applied_at_unix: z.number(),
  updated_at_unix: z.number(),
  // Phase 1 of the reset-quota plan: per-agent quota usage surfaced
  // on the client detail page. Older agents / panels (Telemt < 3.4.6,
  // or backend still mid-rollout) won't send these — default to 0 so
  // schema parse stays green and the UI can decide how to render zero.
  quota_used_bytes: z.number().default(0),
  quota_last_reset_unix: z.number().default(0),
  // Phase 3 of the reset-quota plan: panel's record of the last
  // successful reset for this (client, agent), plus a drift flag
  // computed server-side when the panel's record is newer than what
  // Telemt currently reports (reset job succeeded but Telemt's
  // persisted state has fallen behind). Both default to 0 / false so
  // older backends still produce parseable rows.
  panel_last_reset_unix: z.number().default(0),
  quota_reset_drift: z.boolean().default(false),
});

/**
 * Row shape for GET /api/clients (list view). Distinct from clientSchema
 * which is the detail shape (more fields, includes secret + deployments).
 */
export const clientListItemSchema = z.object({
  id,
  name: z.string(),
  enabled: z.boolean(),
  assigned_nodes_count: z.number(),
  expiration_rfc3339: z.string(),
  traffic_used_bytes: z.number(),
  unique_ips_used: z.number(),
  active_tcp_conns: z.number(),
  data_quota_bytes: z.number(),
  last_deploy_status: z.string(),
});

export const clientListSchema = z.array(clientListItemSchema);

/**
 * Full client DTO returned from GET /api/clients/{id}. The MTProxy
 * secret is only embedded on disclosure-opted endpoints (POST create,
 * POST rotate-secret); the plain GET path strips it server-side and
 * the JSON tag is `omitempty`, so the field is missing from the wire
 * payload entirely. Default it to "" so the schema parses cleanly on
 * the read path while still typing as a required string downstream
 * (SecretSection renders an "ask to reveal" state for the empty case).
 * Without this, the GET response was silently rejected by zod →
 * useClientDetail.query.data stayed undefined → the detail page hung
 * on the loading spinner forever (no console log in production builds).
 */
export const clientSchema = z.object({
  id,
  name: z.string(),
  secret: z.string().default(""),
  user_ad_tag: z.string(),
  enabled: z.boolean(),
  traffic_used_bytes: z.number(),
  unique_ips_used: z.number(),
  active_tcp_conns: z.number(),
  max_tcp_conns: z.number(),
  max_unique_ips: z.number(),
  data_quota_bytes: z.number(),
  expiration_rfc3339: z.string(),
  fleet_group_ids: z.array(z.string()),
  agent_ids: z.array(z.string()),
  deployments: z.array(clientDeploymentSchema),
  created_at_unix: z.number(),
  updated_at_unix: z.number(),
  deleted_at_unix: z.number(),
});

export type ClientParsed = z.infer<typeof clientSchema>;
export type ClientListItemParsed = z.infer<typeof clientListItemSchema>;
