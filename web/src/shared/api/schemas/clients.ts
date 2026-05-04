import { z } from "zod";

/**
 * R-Q-20: Zod schemas for the clients / discovered-clients endpoints
 * that were not covered by the existing schemas/client.ts file.
 *
 * clientSchema / clientListSchema / discoveredClientListSchema are in
 * ./client.ts and ./discovered.ts respectively; this file adds the
 * remaining response shapes used by clientsApi mutations.
 */

export const adoptDiscoveredClientResponseSchema = z.object({
  client_id: z.string(),
  name: z.string(),
});

const bulkAdoptResultStatusSchema = z.enum(["adopted", "already_adopted", "error"]);

const bulkAdoptResultSchema = z.object({
  id: z.string(),
  status: bulkAdoptResultStatusSchema,
  client_id: z.string().optional(),
  name: z.string().optional(),
  message: z.string().optional(),
});

export const bulkAdoptDiscoveredResponseSchema = z.object({
  results: z.array(bulkAdoptResultSchema),
  adopted_count: z.number(),
  already_adopted_count: z.number(),
  error_count: z.number(),
  skipped_out_of_scope: z.number().optional(),
});

const clientIPEntrySchema = z.object({
  ip_address: z.string(),
  first_seen: z.string(),
  last_seen: z.string(),
  country_code: z.string().optional(),
  country_name: z.string().optional(),
  city: z.string().optional(),
  asn: z.string().optional(),
});

export const clientIPHistoryResponseSchema = z.object({
  ips: z.array(clientIPEntrySchema),
  total_unique: z.number(),
});

const bulkClientFailureSchema = z.object({
  id: z.string(),
  error: z.string(),
});

export const bulkClientResponseSchema = z.object({
  action: z.enum(["enable", "disable", "delete"]),
  succeeded: z.array(z.string()),
  skipped: z.array(z.string()),
  failed: z.array(bulkClientFailureSchema),
});

export type AdoptDiscoveredClientResponseParsed = z.infer<typeof adoptDiscoveredClientResponseSchema>;
export type BulkAdoptDiscoveredResponseParsed = z.infer<typeof bulkAdoptDiscoveredResponseSchema>;
export type ClientIPHistoryResponseParsed = z.infer<typeof clientIPHistoryResponseSchema>;
export type BulkClientResponseParsed = z.infer<typeof bulkClientResponseSchema>;
