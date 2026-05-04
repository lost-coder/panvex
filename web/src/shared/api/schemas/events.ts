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
  data: z.unknown(),
});

export type EventEnvelope = z.infer<typeof eventEnvelopeSchema>;
