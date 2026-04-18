import { useQueryClient } from "@tanstack/react-query";
import { createContext, useContext, useEffect, useMemo, useRef, useState } from "react";

import { apiClient } from "@/lib/api";
import { invalidateTelemetryQueries } from "@/lib/telemetry-query-invalidation";
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

// P2-UX-10: expose connection state + "last update" timestamp so the UI can
// surface a reconnection banner and a subtle flash when data arrives via WS.
export type WsStatus = "connecting" | "open" | "reconnecting" | "closed";

export interface WsContextValue {
  status: WsStatus;
  /** Timestamp (ms) of the most recent inbound relevant event. */
  lastEventAt: number | null;
  /** Attempt count for the current reconnect loop (0 while healthy). */
  reconnectAttempts: number;
}

const WsContext = createContext<WsContextValue>({
  status: "connecting",
  lastEventAt: null,
  reconnectAttempts: 0,
});

export function useWsStatus(): WsContextValue {
  return useContext(WsContext);
}

export function EventsSynchronizer({ children }: { children?: React.ReactNode }) {
  const queryClient = useQueryClient();
  const [status, setStatus] = useState<WsStatus>("connecting");
  const [lastEventAt, setLastEventAt] = useState<number | null>(null);
  const [reconnectAttempts, setReconnectAttempts] = useState(0);
  // Refs so the long-lived effect doesn't depend on state and re-subscribe.
  const attemptsRef = useRef(0);

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
      await queryClient.invalidateQueries({ queryKey: ["audit"] });
      await queryClient.invalidateQueries({ queryKey: ["jobs"] });
      await invalidateTelemetryQueries(queryClient);
    };
    const scheduleReconnect = () => {
      if (stopped || reconnectTimerID !== null) { return; }
      setStatus("reconnecting");
      attemptsRef.current += 1;
      setReconnectAttempts(attemptsRef.current);
      reconnectTimerID = window.setTimeout(() => {
        reconnectTimerID = null;
        reconnectDelayMs = Math.min(reconnectDelayMs * 2, 30_000);
        connect();
      }, reconnectDelayMs);
    };
    const connect = () => {
      if (stopped) { return; }
      // Don't open a WebSocket on the login page — there's no session yet.
      if (window.location.pathname.endsWith("/login")) { return; }
      setStatus("connecting");
      const rootPath = resolveConfiguredRootPath();
      const url = buildEventsURL(window.location.protocol, window.location.host, rootPath);
      socket = new WebSocket(url);
      socket.onopen = () => {
        reconnectDelayMs = 1_000;
        attemptsRef.current = 0;
        setReconnectAttempts(0);
        setStatus("open");
      };
      socket.onmessage = (message) => {
        let event: EventEnvelope;
        try { event = JSON.parse(message.data as string) as EventEnvelope; } catch { return; }
        if (!isRelevantEvent(event.type)) { return; }
        setLastEventAt(Date.now());
        void invalidateLiveQueries();
      };
      socket.onerror = () => { socket?.close(); };
      socket.onclose = () => {
        // Already on the login page — nothing to reconnect or redirect to.
        if (window.location.pathname.endsWith("/login")) {
          stopped = true;
          setStatus("closed");
          return;
        }
        // Check if session is still valid before reconnecting.
        // If expired, redirect to login instead of looping with 401s.
        apiClient.me().then(() => {
          scheduleReconnect();
        }).catch(() => {
          stopped = true;
          setStatus("closed");
          const rootPath = resolveConfiguredRootPath();
          window.location.href = rootPath ? `${rootPath}/login` : "/login";
        });
      };
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

  const value = useMemo<WsContextValue>(
    () => ({ status, lastEventAt, reconnectAttempts }),
    [status, lastEventAt, reconnectAttempts],
  );

  // Back-compat: when rendered without children, behave like the old
  // null-returning version (some bootstrap code still uses that form).
  if (children === undefined) {
    return <WsContext.Provider value={value}>{null}</WsContext.Provider>;
  }
  return <WsContext.Provider value={value}>{children}</WsContext.Provider>;
}
