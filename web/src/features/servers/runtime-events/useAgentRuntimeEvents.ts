import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";

import { apiClient } from "@/shared/api/api";
import type { RuntimeEvent } from "@/shared/api/types-runtime-events";
import {
  buildEventsURL,
  resolveConfiguredRootPath,
} from "@/shared/lib/runtime-path";

// MAX_EVENTS caps the in-memory ring so an agent that spams warnings
// cannot grow the React state unboundedly. Matches the server-side
// per-agent ring buffer size; once the cap is hit we drop the oldest
// records, preserving newest-first ordering.
const MAX_EVENTS = 500;

// LIVE_DECAY_MS is how long the "Live" badge stays lit after the most
// recent runtime event arrived. Picked at 2s so a steady trickle of
// agent events keeps the badge on continuously while a quiescent
// agent's UI returns to "idle" within a couple of seconds.
const LIVE_DECAY_MS = 2_000;
const LIVE_DECAY_TICK_MS = 500;

interface BusRuntimeEventRecord {
  ts?: string;
  level?: string;
  message?: string;
  fields?: Record<string, string>;
}

interface BusMessage {
  type: string;
  data?: {
    agent_id?: string;
    events?: BusRuntimeEventRecord[];
  };
}

interface UseAgentRuntimeEventsResult {
  events: RuntimeEvent[];
  isLoading: boolean;
  /** True while runtime events have arrived within LIVE_DECAY_MS. */
  isLive: boolean;
}

function normaliseLevel(level: string | undefined): RuntimeEvent["level"] {
  if (level === "warn" || level === "error") return level;
  return "info";
}

/**
 * useAgentRuntimeEvents merges the HTTP backlog from
 * /api/agents/{id}/runtime-events with the live `runtime.events` batch
 * frames published on the /events WebSocket, returning a single
 * newest-first list capped at MAX_EVENTS. The hook intentionally
 * owns its own WebSocket rather than piggybacking on the panel-wide
 * EventsSynchronizer because runtime events are noisy and per-agent —
 * routing every frame through the global invalidation pipeline would
 * thrash React Query keys unrelated to the open detail page.
 *
 * The hook also tracks an `isLive` flag that toggles on whenever a
 * frame arrives and decays back to false after LIVE_DECAY_MS of
 * silence. Consumers can use it to render a small "Live" indicator
 * next to the events list.
 */
export function useAgentRuntimeEvents(agentId: string): UseAgentRuntimeEventsResult {
  const initial = useQuery({
    queryKey: ["runtime-events", "by-agent", agentId],
    queryFn: () => apiClient.listRuntimeEvents(agentId, { limit: MAX_EVENTS }),
    enabled: !!agentId,
  });

  const [events, setEvents] = useState<RuntimeEvent[]>([]);
  const lastEventAtRef = useRef<number>(0);
  const [isLive, setIsLive] = useState(false);

  // Seed local state from the HTTP backlog whenever it arrives. We
  // overwrite rather than merge here because the backlog is a fresh
  // snapshot — any live frames that arrived between the request and
  // the response would also be present in the new payload.
  useEffect(() => {
    if (!initial.data) return;
    setEvents(initial.data.items);
  }, [initial.data]);

  useEffect(() => {
    if (!agentId) return;
    let socket: WebSocket | null = null;
    try {
      const rootPath = resolveConfiguredRootPath();
      const url = buildEventsURL(
        globalThis.location.protocol,
        globalThis.location.host,
        rootPath,
      );
      socket = new WebSocket(url);
    } catch {
      // Defensive: hostile/old browsers may throw on construction.
      // We swallow here so the HTTP-backed list still renders.
      return;
    }

    socket.onmessage = (msg) => {
      let parsed: BusMessage;
      try {
        parsed = JSON.parse(msg.data as string) as BusMessage;
      } catch {
        return;
      }
      if (parsed.type !== "runtime.events") return;
      const data = parsed.data;
      if (!data || data.agent_id !== agentId || !Array.isArray(data.events)) return;
      const incoming: RuntimeEvent[] = data.events.map((record) => ({
        ts: record.ts ?? new Date().toISOString(),
        level: normaliseLevel(record.level),
        message: record.message ?? "",
        fields: record.fields,
      }));
      if (incoming.length === 0) return;
      setEvents((prev) => {
        // Server batches are oldest-first; the list is newest-first.
        const next = [...incoming].reverse().concat(prev);
        if (next.length > MAX_EVENTS) next.length = MAX_EVENTS;
        return next;
      });
      lastEventAtRef.current = Date.now();
      setIsLive(true);
    };

    return () => {
      socket?.close();
    };
  }, [agentId]);

  // Decay the "live" indicator after a period of silence. Polling once
  // every LIVE_DECAY_TICK_MS keeps the UI responsive without a render
  // on every animation frame.
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
