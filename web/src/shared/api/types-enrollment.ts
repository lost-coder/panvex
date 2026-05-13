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
