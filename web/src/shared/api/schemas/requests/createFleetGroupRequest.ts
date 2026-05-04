import { z } from "zod";

/**
 * Mirrors `createFleetGroupRequest` in
 * internal/controlplane/server/http_fleet_groups.go. `description` is
 * server-side-optional (defaults to ""), but the wire shape always
 * sends a string — we mirror that default here so the request body
 * is identical with or without the form field.
 */
export const createFleetGroupRequestSchema = z.object({
  name: z.string().min(1).max(256),
  label: z.string().min(1).max(256),
  description: z.string().max(1024).optional().default(""),
});

export type CreateFleetGroupRequestParsed = z.infer<typeof createFleetGroupRequestSchema>;
