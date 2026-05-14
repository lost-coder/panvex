// Phase-3 runtime events. These are the structured slog-style records
// produced by the agent runtime and forwarded both via
// GET /api/agents/{id}/runtime-events (initial backlog) and the
// /events WebSocket as `runtime.event` frames.
//
// Note: there is an unrelated `RuntimeEvent` type in `./servers.ts`
// representing the telemetry/lifecycle event stream (sequence,
// timestamp_unix, event_type, context). The two shapes are
// intentionally kept separate — they come from different backend
// pipelines, have different identities, and are surfaced in different
// UI sections. This file is the canonical home for the Phase-3
// runtime-event shape; do not collapse the two.

export type RuntimeEventLevel = "info" | "warn" | "error";

export interface RuntimeEvent {
  ts: string;
  level: RuntimeEventLevel;
  message: string;
  fields?: Record<string, string>;
}

export interface RuntimeEventsListResponse {
  items: RuntimeEvent[];
}
