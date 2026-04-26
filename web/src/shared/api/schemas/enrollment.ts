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
