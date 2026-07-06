import { z } from "zod";

// SYNC: internal/controlplane/server/clients_flow.go (clientNameRegex) и
// username-ограничение Telemt. 7.6: единый источник правды для формы
// (ClientFormSheet) и request-валидации (encodeRequest) — раньше форма
// держала собственную копию, а схема разрешала max(256) без regex.
export const CLIENT_NAME_REGEX = /^[A-Za-z0-9_.-]{1,64}$/;

export const clientMutationRequestSchema = z.object({
  name: z.string().min(1).max(64).regex(CLIENT_NAME_REGEX),
  enabled: z.boolean().nullable().optional(),
  user_ad_tag: z.string().max(256).optional().default(""),
  // Tri-state flag: omitted/true → control-plane auto-generates a tag
  // when user_ad_tag is empty; false → store the value literally
  // (empty means no tag).
  user_ad_tag_auto: z.boolean().optional(),
  max_tcp_conns: z.number().int().nonnegative().max(1_000_000).optional().default(0),
  max_unique_ips: z.number().int().nonnegative().max(1_000_000).optional().default(0),
  data_quota_bytes: z.number().int().nonnegative().optional().default(0),
  expiration_rfc3339: z.string().max(64).optional().default(""),
  fleet_group_ids: z.array(z.string().min(1)).optional().default([]),
  agent_ids: z.array(z.string().min(1)).optional().default([]),
});

export type ClientMutationRequest = z.infer<typeof clientMutationRequestSchema>;
