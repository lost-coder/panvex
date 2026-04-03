import { useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";

import { invalidateTelemetryQueries } from "@/features/telemetry/telemetry-query-invalidation";
import { buildEventsURL, resolveConfiguredRootPath } from "@/lib/runtime-path";

type EventEnvelope = {
  type: string;
  data: unknown;
};

function isRelevantEvent(eventType: string): boolean {
  switch (eventType) {
    case "agents.enrolled":
    case "agents.updated":
    case "jobs.created":
    case "audit.created":
      return true;
    default:
      return false;
  }
}

export function EventsSynchronizer() {
  const queryClient = useQueryClient();
  useEffect(() => {
    if (typeof window === "undefined") { return; }
    let socket: WebSocket | null = null;
    let reconnectDelayMs = 1_000;
    let reconnectTimerID: number | null = null;
    let stopped = false;
    const invalidateLiveQueries = async () => {
      await queryClient.invalidateQueries({ queryKey: ["control-room"] });
      await queryClient.invalidateQueries({ queryKey: ["agents"] });
      await queryClient.invalidateQueries({ queryKey: ["clients"] });
      await queryClient.invalidateQueries({ predicate: (query) => query.queryKey[0] === "client" });
      await invalidateTelemetryQueries(queryClient);
    };
    const scheduleReconnect = () => {
      if (stopped || reconnectTimerID !== null) { return; }
      reconnectTimerID = window.setTimeout(() => {
        reconnectTimerID = null;
        reconnectDelayMs = Math.min(reconnectDelayMs * 2, 30_000);
        connect();
      }, reconnectDelayMs);
    };
    const connect = () => {
      if (stopped) { return; }
      const rootPath = resolveConfiguredRootPath();
      const url = buildEventsURL(window.location.protocol, window.location.host, rootPath);
      socket = new WebSocket(url);
      socket.onopen = () => { reconnectDelayMs = 1_000; };
      socket.onmessage = (message) => {
        let event: EventEnvelope;
        try { event = JSON.parse(message.data as string) as EventEnvelope; } catch { return; }
        if (!isRelevantEvent(event.type)) { return; }
        void invalidateLiveQueries();
      };
      socket.onerror = () => { socket?.close(); };
      socket.onclose = () => { scheduleReconnect(); };
    };
    connect();
    return () => {
      stopped = true;
      if (reconnectTimerID !== null) { window.clearTimeout(reconnectTimerID); }
      if (socket !== null && (socket.readyState === WebSocket.OPEN || socket.readyState === WebSocket.CONNECTING)) {
        socket.close();
      }
    };
  }, [queryClient]);
  return null;
}
