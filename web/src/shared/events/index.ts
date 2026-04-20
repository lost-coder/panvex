// Phase 4d: shared realtime-event helpers.
export { invalidationsForEvent, isKnownEventType } from "./event-invalidations";
export type { EventEnvelope, EventInvalidation } from "./event-invalidations";
export { invalidateTelemetryQueries } from "./telemetry-query-invalidation";
