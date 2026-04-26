import { z } from "zod";

import { id, unixSeconds } from "./common.ts";

/**
 * R-Q-20: Zod schemas for /fleet-groups + the deletion-preview shape.
 * Enrollment-token schemas live in ./enrollment.ts so the per-domain
 * schema files map 1:1 onto the per-domain api modules.
 *
 * The schemas mirror the runtime types declared in
 * shared/api/fleet-groups.ts 1:1 (provider_id is `string | undefined`
 * to satisfy exactOptionalPropertyTypes against the existing type).
 */

export const fleetGroupIntegrationSchema = z.object({
  id,
  kind: z.string(),
  provider_id: z.string().or(z.undefined()),
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

export type FleetGroupParsed = z.infer<typeof fleetGroupSchema>;
export type FleetGroupIntegrationParsed = z.infer<typeof fleetGroupIntegrationSchema>;
