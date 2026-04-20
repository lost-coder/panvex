import { z } from "zod";

import { id } from "./common.ts";

const clientDeploymentSchema = z.object({
  agent_id: z.string(),
  desired_operation: z.string(),
  status: z.string(),
  last_error: z.string(),
  connection_link: z.string(),
  last_applied_at_unix: z.number(),
  updated_at_unix: z.number(),
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
 * Full client DTO returned from GET /api/clients/{id}. Contains the
 * MTProxy secret so this response must never be logged.
 */
export const clientSchema = z.object({
  id,
  name: z.string(),
  secret: z.string(),
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
