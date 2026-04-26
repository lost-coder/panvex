import { z } from "zod";

import { id, unixSeconds } from "./common.ts";

/**
 * R-Q-20: Zod schemas for /fleet-groups and /enrollment-tokens.
 */

export const fleetGroupIntegrationSchema = z.object({
  id,
  kind: z.string(),
  provider_id: z.string().optional(),
  enabled: z.boolean(),
  config: z.unknown(),
  created_at_unix: unixSeconds,
  updated_at_unix: unixSeconds,
});

export const fleetGroupSchema = z.object({
  id,
  name: z.string(),
  label: z.string(),
  description: z.string(),
  agent_count: z.number().int(),
  created_at_unix: unixSeconds,
  updated_at_unix: unixSeconds,
  integrations: z.array(fleetGroupIntegrationSchema),
});

export const fleetGroupListSchema = z.array(fleetGroupSchema);

export const fleetGroupDeletionPreviewSchema = z.object({
  id,
  agent_count: z.number().int(),
  enrollment_token_count: z.number().int(),
  client_assignment_count: z.number().int(),
  reassign_required: z.boolean(),
});

export const enrollmentTokenSchema = z.object({
  value: z.string(),
  fleet_group_id: z.string().optional(),
  status: z.enum(["active", "consumed", "expired", "revoked"]),
  issued_at_unix: unixSeconds,
  expires_at_unix: unixSeconds,
});

export const enrollmentTokenListSchema = z.array(enrollmentTokenSchema);

export type FleetGroupParsed = z.infer<typeof fleetGroupSchema>;
export type FleetGroupIntegrationParsed = z.infer<typeof fleetGroupIntegrationSchema>;
export type EnrollmentTokenParsed = z.infer<typeof enrollmentTokenSchema>;
