import { z } from "zod";

export const clientMutationRequestSchema = z.object({
  name: z.string().min(1).max(256),
  enabled: z.boolean().nullable().optional(),
  user_ad_tag: z.string().max(256).optional().default(""),
  max_tcp_conns: z.number().int().nonnegative().max(1_000_000).optional().default(0),
  max_unique_ips: z.number().int().nonnegative().max(1_000_000).optional().default(0),
  data_quota_bytes: z.number().int().nonnegative().optional().default(0),
  expiration_rfc3339: z.string().max(64).optional().default(""),
  fleet_group_ids: z.array(z.string().min(1)).optional().default([]),
  agent_ids: z.array(z.string().min(1)).optional().default([]),
});

export type ClientMutationRequest = z.infer<typeof clientMutationRequestSchema>;
