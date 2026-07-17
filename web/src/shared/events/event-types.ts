// SYNC: internal/controlplane/events/types.go — the Go source of the event
// taxonomy (P3-3.3, audit #22). The list check is done by
// web/scripts/check-event-parity.mjs (CI: npm run check:events); it
// parses the array below line-by-line by "…" literals, so keep every element
// on its own line.
export const EVENT_TYPES = [
  "agents.enrolled",
  "agents.updated",
  "audit.created",
  "clients.updated",
  "enrollment.completed",
  "enrollment.event",
  "enrollment.failed",
  "jobs.created",
  "runtime.events",
] as const;

export type EventType = (typeof EVENT_TYPES)[number];

export function isKnownEventType(type: string): type is EventType {
  return (EVENT_TYPES as readonly string[]).includes(type);
}
