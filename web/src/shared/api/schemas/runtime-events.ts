// Task 2.8 (audit, MEDIUM M13 + H3 hole): Zod schemas for the Phase-3
// runtime-event shape — the one remaining WS/HTTP consumer that parsed
// its payload with an unchecked `as` cast instead of going through the
// zod-guarded `api<>()` / EventsSynchronizer pattern used everywhere
// else.
//
// Two call sites share this shape:
//   1. GET /api/agents/{id}/runtime-events — the HTTP backlog seed
//      (see shared/api/runtime-events.ts), validated via `api<>()`'s
//      3rd-arg schema.
//   2. The `runtime.events` WebSocket bus frame — validated by
//      useAgentRuntimeEvents.ts via `runtimeEventsBusFrameSchema`
//      before touching React state, mirroring EventsSynchronizer's
//      `eventEnvelopeSchema.safeParse` pattern.
//
// Mirrors the `RuntimeEvent` / `RuntimeEventsListResponse` TS
// interfaces in `shared/api/types-runtime-events.ts` exactly.
import { z } from "zod";

export const runtimeEventRecordSchema = z.object({
  ts: z.string(),
  level: z.enum(["info", "warn", "error"]),
  message: z.string(),
  // Optional: absent on most records; present when the agent attaches
  // structured slog fields (e.g. error causes, connection ids).
  fields: z.record(z.string(), z.string()).optional(),
});

export type RuntimeEventRecordParsed = z.infer<typeof runtimeEventRecordSchema>;

export const runtimeEventsListResponseSchema = z.object({
  items: z.array(runtimeEventRecordSchema),
});

export type RuntimeEventsListResponseParsed = z.infer<
  typeof runtimeEventsListResponseSchema
>;

// runtimeEventsBusFrameSchema validates the `/events` WebSocket envelope
// for `runtime.events` frames specifically: `{ type, data: { agent_id,
// events } }`. Unlike the generic `eventEnvelopeSchema` (whose `data` is
// intentionally `unknown`), useAgentRuntimeEvents reads deep into
// `data.events[].ts` synchronously in `onmessage` — so this frame needs
// its own fully-typed schema rather than the shared envelope.
//
// Individual malformed *elements* (e.g. `events: [null]`) fail
// `z.array(runtimeEventRecordSchema)` and drop the whole frame; the
// hook already tolerates dropped frames because the HTTP backlog will
// have carried the same data down the wire on the next poll/reconnect.
export const runtimeEventsBusFrameSchema = z.object({
  type: z.literal("runtime.events"),
  data: z.object({
    agent_id: z.string(),
    events: z.array(runtimeEventRecordSchema),
  }),
});

export type RuntimeEventsBusFrameParsed = z.infer<
  typeof runtimeEventsBusFrameSchema
>;
