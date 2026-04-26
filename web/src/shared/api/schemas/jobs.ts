import { z } from "zod";

import { id, timestamp } from "./common.ts";

/**
 * R-Q-20: Zod schemas for the activity-log endpoints (/jobs, /audit).
 *
 * Defensive shape: every field the UI reads is validated; unknown
 * fields pass through so backend additions do not block a release.
 */

export const jobTargetSchema = z.object({
  agent_id: id,
  status: z.string(),
  result_text: z.string().optional().default(""),
  result_json: z.string().optional().default(""),
  updated_at: timestamp,
});

export const jobSchema = z.object({
  id,
  action: z.string(),
  target_agent_ids: z.array(id),
  targets: z.array(jobTargetSchema),
  /** TTL in nanoseconds (Go time.Duration). */
  ttl: z.number(),
  idempotency_key: z.string(),
  actor_id: z.string(),
  status: z.string(),
  payload_json: z.string(),
  created_at: timestamp,
});

export const jobListSchema = z.array(jobSchema);

export const auditEventSchema = z.object({
  id,
  actor_id: z.string(),
  action: z.string(),
  target_id: z.string(),
  created_at: timestamp,
  // The backend writes whatever map[string]any an audit-emitter passed
  // in — schema-validating each shape would mean tracking 30+ event
  // types here. record(unknown) keeps the contract honest while
  // letting consumers narrow on `action`.
  details: z.record(z.string(), z.unknown()),
});

export const auditEventListSchema = z.array(auditEventSchema);

export type JobParsed = z.infer<typeof jobSchema>;
export type JobTargetParsed = z.infer<typeof jobTargetSchema>;
export type AuditEventParsed = z.infer<typeof auditEventSchema>;
