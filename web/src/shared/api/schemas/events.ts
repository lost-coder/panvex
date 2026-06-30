// Q-10 (audit, HIGH): Zod schema for the WebSocket EventEnvelope.
//
// The WS message decode path used to coerce JSON output to the typed
// `EventEnvelope` shape with no runtime validation
// (`JSON.parse(message.data) as EventEnvelope`). That's the same DF-10
// failure mode we hardened the HTTP API layer against — a malformed or
// hostile payload would flow through the synchronizer with a TypeScript
// shape it doesn't actually have, and crash downstream consumers.
//
// We mirror the existing `EventEnvelope` interface (see
// `shared/events/event-invalidations.ts`) exactly: `{ type: string,
// data: unknown }`. The payload is intentionally `unknown` because the
// shape varies per event family and downstream helpers
// (`extractAgentID`, `invalidationsForEvent`) already handle untyped
// data defensively.
import { z } from "zod";

export const eventEnvelopeSchema = z.object({
  type: z.string(),
  // `.optional()` is required since zod 4.4: a bare `z.unknown()` key is no
  // longer implicitly optional, so a missing `data` would fail with
  // "expected nonoptional". The payload stays `unknown` (shape varies per
  // event family; downstream helpers handle untyped/absent data defensively).
  data: z.unknown().optional(),
  // seq is the hub-assigned global sequence number (D6c). Optional so a
  // panel/dashboard version skew never drops every event on the floor.
  seq: z.number().int().nonnegative().optional(),
});

export type EventEnvelope = z.infer<typeof eventEnvelopeSchema>;
