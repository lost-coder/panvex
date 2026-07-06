import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";

import { apiClient } from "@/shared/api/api";
import { useWsEvents, useWsStatus } from "@/app/providers/EventsSynchronizer";
import { runtimeEventsBusFrameSchema } from "@/shared/api/schemas/runtime-events";
import type { RuntimeEvent } from "@/shared/api/types-runtime-events";

// MAX_EVENTS caps the in-memory ring so an agent that spams warnings
// cannot grow the React state unboundedly. Matches the server-side
// per-agent ring buffer size; once the cap is hit we drop the oldest
// records, preserving newest-first ordering.
const MAX_EVENTS = 500;

// LIVE_DECAY_MS is how long the "Live" badge stays lit after the most
// recent runtime event arrived.
const LIVE_DECAY_MS = 2_000;
const LIVE_DECAY_TICK_MS = 500;

interface UseAgentRuntimeEventsResult {
  events: RuntimeEvent[];
  isLoading: boolean;
  /** True while runtime events have arrived within LIVE_DECAY_MS. */
  isLive: boolean;
}

/**
 * useAgentRuntimeEvents merges the HTTP backlog from
 * /api/agents/{id}/runtime-events with live `runtime.events` batch
 * frames, returning a single newest-first list capped at MAX_EVENTS.
 *
 * 7.4 (#web-6): the hook used to own a SECOND WebSocket to /events with
 * no onclose/onerror/reconnect — a dropped connection silently killed
 * the live feed. The server broadcasts the whole event bus to every
 * /events subscriber (no per-topic subscription exists), so the second
 * socket duplicated traffic for nothing. The hook now subscribes to the
 * panel-wide EventsSynchronizer socket via useWsEvents() and filters
 * `runtime.events` frames by agent_id client-side; reconnect/backoff/
 * session-probe come for free. On every reconnect (status transition
 * → "open") the HTTP backlog query is invalidated so records missed
 * while the socket was down are recovered.
 */
export function useAgentRuntimeEvents(agentId: string): UseAgentRuntimeEventsResult {
  const initial = useQuery({
    queryKey: ["runtime-events", "by-agent", agentId],
    queryFn: ({ signal }) => apiClient.listRuntimeEvents(agentId, { limit: MAX_EVENTS }, { signal }),
    enabled: !!agentId,
  });

  const { subscribe } = useWsEvents();
  const { status } = useWsStatus();
  const queryClient = useQueryClient();

  const [events, setEvents] = useState<RuntimeEvent[]>([]);
  const lastEventAtRef = useRef<number>(0);
  const [isLive, setIsLive] = useState(false);

  // Seed local state from the HTTP backlog whenever it arrives. We
  // overwrite rather than merge here because the backlog is a fresh
  // snapshot — any live frames that arrived between the request and
  // the response would also be present in the new payload.
  useEffect(() => {
    if (!initial.data) return;
    // eslint-disable-next-line react-hooks/set-state-in-effect -- канонический sync-from-query паттерн, был здесь и до 7.4
    setEvents(initial.data.items);
  }, [initial.data]);

  useEffect(() => {
    if (!agentId) return;
    return subscribe((envelope) => {
      if (envelope.type !== "runtime.events") return;
      // M13: конверт уже прошёл eventEnvelopeSchema в провайдере, но
      // data для runtime-кадров читается глубоко (data.events[].ts) —
      // валидируем полную форму, малформед-кадр дропаем целиком.
      const result = runtimeEventsBusFrameSchema.safeParse(envelope);
      if (!result.success) {
        if (
          import.meta !== undefined &&
          (import.meta as ImportMeta & { env?: { DEV?: boolean } }).env?.DEV
        ) {
          console.debug("runtime-events: malformed bus frame, dropping", result.error);
        }
        return;
      }
      const data = result.data.data;
      if (data.agent_id !== agentId) return;
      const incoming: RuntimeEvent[] = data.events;
      if (incoming.length === 0) return;
      setEvents((prev) => {
        // Server batches are oldest-first; the list is newest-first.
        const next = [...incoming].reverse().concat(prev);
        if (next.length > MAX_EVENTS) next.length = MAX_EVENTS;
        return next;
      });
      lastEventAtRef.current = Date.now();
      setIsLive(true);
    });
  }, [agentId, subscribe]);

  // Reconnect → refetch backlog: кадры, отправленные пока сокет лежал,
  // потеряны навсегда (bus не реиграет) — их закрывает только HTTP-бэклог.
  const prevStatusRef = useRef(status);
  useEffect(() => {
    const prev = prevStatusRef.current;
    prevStatusRef.current = status;
    if (!agentId) return;
    if (status === "open" && prev !== "open") {
      void queryClient.invalidateQueries({
        queryKey: ["runtime-events", "by-agent", agentId],
      });
    }
  }, [status, agentId, queryClient]);

  // Decay the "live" indicator after a period of silence.
  useEffect(() => {
    if (!isLive) return;
    const id = globalThis.setInterval(() => {
      if (Date.now() - lastEventAtRef.current > LIVE_DECAY_MS) {
        setIsLive(false);
      }
    }, LIVE_DECAY_TICK_MS);
    return () => {
      globalThis.clearInterval(id);
    };
  }, [isLive]);

  return {
    events,
    isLoading: initial.isLoading,
    isLive,
  };
}
