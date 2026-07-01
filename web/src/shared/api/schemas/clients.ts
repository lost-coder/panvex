import { z } from "zod";

import { clientSchema } from "./client.ts";
import { jobSchema } from "./jobs.ts";

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
  // M7 (audit remediation phase 2): false when the backend's
  // CountUniqueClientIPs query failed — total_unique is then a 0
  // placeholder, not a real count. Optional so older/other server
  // builds that omit the field still validate; callers should treat a
  // missing flag as "available" (historical behaviour).
  total_unique_available: z.boolean().optional(),
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

/**
 * Reset-quota Phase 2 response. The backend enqueues a job and returns
 * the refreshed client detail alongside the freshly created Job. Frontend
 * keeps a reference to the job so it can poll /api/jobs until the
 * per-target status reaches a terminal value, then drives per-row UX
 * (toast on success, inline message on unsupported/read-only/generic
 * failure) from the Job.Targets payload.
 */
export const resetQuotaResponseSchema = z.object({
  client: clientSchema,
  job: jobSchema,
});

export const rescanDiscoveredResponseSchema = z.object({
  agents_notified: z.number(),
});

export type AdoptDiscoveredClientResponseParsed = z.infer<typeof adoptDiscoveredClientResponseSchema>;
export type BulkAdoptDiscoveredResponseParsed = z.infer<typeof bulkAdoptDiscoveredResponseSchema>;
export type ClientIPHistoryResponseParsed = z.infer<typeof clientIPHistoryResponseSchema>;
export type BulkClientResponseParsed = z.infer<typeof bulkClientResponseSchema>;
export type ResetQuotaResponseParsed = z.infer<typeof resetQuotaResponseSchema>;
export type RescanDiscoveredResponse = z.infer<typeof rescanDiscoveredResponseSchema>;
