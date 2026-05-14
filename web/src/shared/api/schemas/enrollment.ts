import { z } from "zod";

import { unixSeconds } from "./common.ts";

/**
 * R-Q-20: Zod schemas for /api/agents/enrollment-tokens.
 *
 * Schemas mirror the runtime types declared in shared/api/enrollment.ts
 * 1:1 (including which fields are truly optional) so the api<T>()
 * overload accepts them under exactOptionalPropertyTypes.
 */

// PR-2a: ScriptSource / ScriptSources mirror the OpenAPI schemas.
// `sha256` is intentionally nullable rather than optional — the backend
// emits `null` for the GitHub source (the panel cannot vouch for bytes
// it does not host) so the field is always present on the wire.
export const scriptSourceSchema = z.object({
  url: z.string(),
  sha256: z.string().nullable(),
});

export const scriptSourcesSchema = z.object({
  panel: scriptSourceSchema,
  github: scriptSourceSchema,
});

export const enrollmentTokenResponseSchema = z.object({
  value: z.string(),
  panel_url: z.string(),
  fleet_group_id: z.string(),
  issued_at_unix: unixSeconds,
  expires_at_unix: unixSeconds,
  ca_pem: z.string(),
  script_sources: scriptSourcesSchema,
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

// PR-2c: schemas for POST /api/agents/provision-outbound. The request
// mirrors the OpenAPI ProvisionOutboundAgentRequest; `script_source`
// defaults to "github" on the server when omitted, but we keep it
// optional on the wire so the wizard can leave it unset for the
// out-of-the-box outbound case.
export const provisionOutboundAgentAdvancedSchema = z.object({
  telemt_url: z.string().nullable().optional(),
  telemt_metrics_url: z.string().nullable().optional(),
  telemt_auth: z.string().nullable().optional(),
  insecure_transport: z.boolean().nullable().optional(),
});

export const provisionOutboundAgentRequestSchema = z.object({
  node_name: z.string().min(1).max(64),
  fleet_group_id: z.string(),
  dial_address: z.string().min(1),
  script_source: z.enum(["panel", "github"]).optional(),
  advanced: provisionOutboundAgentAdvancedSchema.optional(),
});

export const provisionOutboundAgentResponseSchema = z.object({
  agent_id: z.string(),
  command: z.string(),
  expires_at_unix: unixSeconds,
  script_url: z.string(),
});
