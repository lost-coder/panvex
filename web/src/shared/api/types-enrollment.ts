// Enrollment attempt timeline types — mirror the JSON shapes produced
// by GET /api/enrollment-attempts and GET /api/enrollment-attempts/{id}.
//
// The Go side projects `enrollment.AttemptDTO` / `EventDTO` directly to
// JSON via the tags declared in internal/controlplane/enrollment/recorder.go.
// Optional columns use `omitempty`, so we widen those fields with
// `| undefined` here to stay compatible with exactOptionalPropertyTypes.

export type EnrollmentMode = "inbound" | "outbound";
export type EnrollmentStatus = "in_progress" | "success" | "failed";
export type EnrollmentLevel = "info" | "warn" | "error";

export interface EnrollmentAttempt {
  id: string;
  token_id?: string | undefined;
  agent_id?: string | undefined;
  mode: EnrollmentMode;
  client_addr?: string | undefined;
  request_id: string;
  status: EnrollmentStatus;
  error_code?: string | undefined;
  error_message?: string | undefined;
  started_at: string;
  finished_at?: string | undefined;
}

export interface EnrollmentEvent {
  step: string;
  level: EnrollmentLevel;
  message?: string | undefined;
  fields?: Record<string, unknown> | undefined;
  ts: string;
}

export interface EnrollmentAttemptDetail {
  attempt: EnrollmentAttempt;
  events: EnrollmentEvent[];
}

// Phase-3 §3.b: filter shape for GET /api/enrollment-attempts. The
// backend treats every field as optional; absent params restore the
// previous default (latest 20 attempts across the fleet). `cursor` is
// the opaque base64 token returned by a prior call and threads
// pagination across calls.
export interface EnrollmentAttemptsFilter {
  token_id?: string;
  agent_id?: string;
  status?: EnrollmentStatus;
  mode?: EnrollmentMode;
  error_code?: string;
  started_after?: string;
  started_before?: string;
  limit?: number;
  cursor?: string;
}

// EnrollmentAttemptsPage mirrors the cursor-paginated response shape
// the server emits: `next_cursor` is `null` (not omitted) when the
// caller has reached the end, so we model it as `string | null` rather
// than optional. Earlier Phase-1 callers that only read `.items` keep
// working — the extra field is additive.
export interface EnrollmentAttemptsPage {
  items: EnrollmentAttempt[];
  next_cursor: string | null;
}
