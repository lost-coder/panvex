import { z } from "zod";

import { unixSeconds } from "./common.ts";

/**
 * R-Q-20: Zod schemas for /api/agents/enrollment-tokens.
 *
 * Schemas mirror the runtime types declared in shared/api/enrollment.ts
 * 1:1 (including which fields are truly optional) so the api<T>()
 * overload accepts them under exactOptionalPropertyTypes.
 */

export const enrollmentTokenResponseSchema = z.object({
  value: z.string(),
  panel_url: z.string(),
  fleet_group_id: z.string(),
  issued_at_unix: unixSeconds,
  expires_at_unix: unixSeconds,
  ca_pem: z.string(),
});

export const enrollmentTokenListItemSchema = z.object({
  value: z.string(),
  panel_url: z.string(),
  fleet_group_id: z.string(),
  status: z.enum(["active", "expired", "consumed", "revoked"]),
  issued_at_unix: unixSeconds,
  expires_at_unix: unixSeconds,
  // Both timestamps are JSON-omitempty on the backend so the field is
  // truly optional (absent — not undefined) in the wire payload.
  consumed_at_unix: unixSeconds.optional(),
  revoked_at_unix: unixSeconds.optional(),
});

export const enrollmentTokenListSchema = z.array(enrollmentTokenListItemSchema);

export type EnrollmentTokenResponseParsed = z.infer<typeof enrollmentTokenResponseSchema>;
export type EnrollmentTokenListItemParsed = z.infer<typeof enrollmentTokenListItemSchema>;

// Enrollment-attempt observability schemas (Phase-1, Tasks 23+).
// Mirror the JSON projections in internal/controlplane/enrollment/recorder.go:
// `AttemptDTO`, `EventDTO`, `AttemptWithEvents`. Optional fields are
// `omitempty` on the Go side, so we mark them optional here too.

export const enrollmentAttemptSchema = z.object({
  id: z.string(),
  token_id: z.string().optional(),
  agent_id: z.string().optional(),
  mode: z.enum(["inbound", "outbound"]),
  client_addr: z.string().optional(),
  request_id: z.string(),
  status: z.enum(["in_progress", "success", "failed"]),
  error_code: z.string().optional(),
  error_message: z.string().optional(),
  started_at: z.string(),
  finished_at: z.string().optional(),
});

export const enrollmentEventSchema = z.object({
  step: z.string(),
  level: z.enum(["info", "warn", "error"]),
  message: z.string().optional(),
  fields: z.record(z.string(), z.unknown()).optional(),
  ts: z.string(),
});

export const enrollmentAttemptListResponseSchema = z.object({
  items: z.array(enrollmentAttemptSchema),
});

export const enrollmentAttemptDetailSchema = z.object({
  attempt: enrollmentAttemptSchema,
  events: z.array(enrollmentEventSchema),
});
