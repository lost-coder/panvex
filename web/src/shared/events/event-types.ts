// SYNC: internal/controlplane/events/types.go — Go-источник событийной
// таксономии (P3-3.3, аудит #22). Сверку списков делает
// web/scripts/check-event-parity.mjs (CI: npm run check:events); он
// парсит массив ниже построчно по литералам "…", поэтому каждый элемент
// держи на своей строке.
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
