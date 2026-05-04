import { z } from "zod";

/**
 * Mirrors `updateFleetGroupRequest` in
 * internal/controlplane/server/http_fleet_groups.go. Only `label`
 * and `description` are mutable post-create; `name` is the immutable
 * URL slug.
 */
export const updateFleetGroupRequestSchema = z.object({
  label: z.string().min(1).max(256),
  description: z.string().max(1024).optional().default(""),
});

export type UpdateFleetGroupRequestParsed = z.infer<typeof updateFleetGroupRequestSchema>;
