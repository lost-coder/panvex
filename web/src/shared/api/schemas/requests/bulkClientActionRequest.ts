import { z } from "zod";

/**
 * Mirrors `bulkClientRequest` in
 * internal/controlplane/server/http_clients.go. The server caps the
 * id list at 500 entries (`bulkClientMaxIDs`); enforcing the same
 * bound here lets the UI surface "too many" without the round-trip.
 */
export const bulkClientActionRequestSchema = z.object({
  action: z.enum(["enable", "disable", "delete"]),
  ids: z.array(z.string().min(1)).min(1).max(500),
});

export type BulkClientActionRequest = z.infer<typeof bulkClientActionRequestSchema>;
