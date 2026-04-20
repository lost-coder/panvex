import { z } from "zod";

import { id } from "./common.ts";

const discoveredClientConflictSchema = z.object({
  type: z.enum(["same_secret_different_names", "same_name_different_secrets"]),
  related_ids: z.array(z.string()),
});

/**
 * DTO returned by GET /api/discovered-clients. A discovered client is an
 * MTProxy client that an agent saw locally but that is not yet adopted
 * into the panel's central registry.
 */
export const discoveredClientSchema = z.object({
  id,
  agent_id: z.string(),
  node_name: z.string(),
  client_name: z.string(),
  status: z.enum(["pending_review", "adopted", "ignored"]),
  total_octets: z.number(),
  current_connections: z.number(),
  active_unique_ips: z.number(),
  connection_link: z.string(),
  max_tcp_conns: z.number(),
  max_unique_ips: z.number(),
  data_quota_bytes: z.number(),
  expiration: z.string(),
  discovered_at_unix: z.number(),
  updated_at_unix: z.number(),
  conflicts: z.array(discoveredClientConflictSchema).optional(),
});

export const discoveredClientListSchema = z.array(discoveredClientSchema);

export type DiscoveredClientParsed = z.infer<typeof discoveredClientSchema>;
