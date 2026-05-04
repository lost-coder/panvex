import { z } from "zod";

/**
 * Mirrors `bulkAdoptRequest` in
 * internal/controlplane/server/http_clients_discovery.go. Server cap
 * is `maxBulkAdoptIDs = 10_000`; the UI never approaches that, but we
 * mirror the bound so a runaway caller fails loudly client-side.
 */
export const bulkAdoptDiscoveredRequestSchema = z.object({
  ids: z.array(z.string().min(1)).min(1).max(10_000),
});

export type BulkAdoptDiscoveredRequest = z.infer<typeof bulkAdoptDiscoveredRequestSchema>;
