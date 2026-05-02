import { z } from "zod";

import { fleetGroupDeletionPreviewSchema } from "./fleet.ts";

/**
 * R-Q-20: Zod schemas for the fleet-groups mutation/read endpoints that
 * were not covered by the existing schemas/fleet.ts file.
 *
 * fleetGroupSchema / fleetGroupListSchema / fleetGroupDeletionPreviewSchema /
 * fleetGroupIntegrationSchema are in ./fleet.ts; this file adds the
 * remaining response shapes used by fleetGroupsApi.
 */

export const fleetGroupDeletionResultSchema = z.object({
  moved: fleetGroupDeletionPreviewSchema,
});

export const integrationKindSchema = z.object({
  name: z.string(),
  description: z.string(),
  provider_kind: z.string().optional(),
});

export const integrationKindListSchema = z.array(integrationKindSchema);

export const integrationProviderKindSchema = z.object({
  name: z.string(),
  description: z.string(),
});

export const integrationProviderKindListSchema = z.array(integrationProviderKindSchema);

export const integrationProviderSchema = z.object({
  id: z.string(),
  kind: z.string(),
  label: z.string(),
  config: z.unknown(),
  created_at_unix: z.number(),
  updated_at_unix: z.number(),
});

export const integrationProviderListSchema = z.array(integrationProviderSchema);

export type FleetGroupDeletionResultParsed = z.infer<typeof fleetGroupDeletionResultSchema>;
export type IntegrationKindParsed = z.infer<typeof integrationKindSchema>;
export type IntegrationProviderKindParsed = z.infer<typeof integrationProviderKindSchema>;
export type IntegrationProviderParsed = z.infer<typeof integrationProviderSchema>;
