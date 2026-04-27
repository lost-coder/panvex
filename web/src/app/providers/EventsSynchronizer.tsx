import { useQueryClient } from "@tanstack/react-query";
import { createContext, useContext, useEffect, useMemo, useRef, useState } from "react";

import { apiClient, SESSION_EXPIRED_EVENT } from "@/shared/api/api";
import { invalidateTelemetryQueries } from "@/shared/events/telemetry-query-invalidation";
import { buildEventsURL, resolveConfiguredRootPath } from "@/shared/lib/runtime-path";
import {
  type EventEnvelope,
  type EventInvalidation,
  invalidationsForEvent,
  isKnownEventType,
} from "@/shared/events/event-invalidations";

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

// R-Q-24: provider co-locates the hook by convention; see AppearanceProvider.
// eslint-disable-next-line react-refresh/only-export-components
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
    if (globalThis.window === undefined) { return; }
    let socket: WebSocket | null = null;
    let reconnectDelayMs = 1_000;
    let reconnectTimerID: ReturnType<typeof setTimeout> | null = null;
    let stopped = false;
    const applyInvalidation = async (invalidation: EventInvalidation) => {
      for (const key of invalidation.keys) {
        await queryClient.invalidateQueries({ queryKey: key as unknown[] });
        // "clients" sweep also refreshes per-client detail queries
        // (["client", id]) so the detail view updates in-flight.
        if (key[0] === "clients") {
          await queryClient.invalidateQueries({
            predicate: (query) => query.queryKey[0] === "client",
          });
        }
      }
      if (invalidation.telemetry) {
        invalidateTelemetryQueries(queryClient, invalidation.telemetryAgentID);
      }
    };
    const scheduleReconnect = () => {
      if (stopped || reconnectTimerID !== null) { return; }
      setStatus("reconnecting");
      attemptsRef.current += 1;
      setReconnectAttempts(attemptsRef.current);
      reconnectTimerID = globalThis.setTimeout(() => {
        reconnectTimerID = null;
        reconnectDelayMs = Math.min(reconnectDelayMs * 2, 30_000);
        connect();
      }, reconnectDelayMs);
    };
    const connect = () => {
      if (stopped) { return; }
      // Don't open a WebSocket on the login page — there's no session yet.
      if (globalThis.location.pathname.endsWith("/login")) { return; }
      setStatus("connecting");
      const rootPath = resolveConfiguredRootPath();
      const url = buildEventsURL(globalThis.location.protocol, globalThis.location.host, rootPath);
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
        if (typeof event?.type !== "string") { return; }
        if (!isKnownEventType(event.type) && console !== undefined) {
          console.debug("events: unknown event type, falling back to broad sweep", event.type);
        }
        setLastEventAt(Date.now());
        void applyInvalidation(invalidationsForEvent(event));
      };
      socket.onerror = () => { socket?.close(); };
      socket.onclose = () => {
        // Already on the login page — nothing to reconnect or redirect to.
        if (globalThis.location.pathname.endsWith("/login")) {
          stopped = true;
          setStatus("closed");
          return;
        }
        // Q3.U-Q-22: check session validity before reconnecting. On
        // expiration we dispatch SESSION_EXPIRED_EVENT so AuthProvider
        // owns the router navigation + cache clear + toast — instead of
        // a hard globalThis.location.href that bypasses React Query and
        // misses the toast announcement.
        apiClient.me().then(() => {
          scheduleReconnect();
        }).catch(() => {
          stopped = true;
          setStatus("closed");
          globalThis.dispatchEvent(new CustomEvent(SESSION_EXPIRED_EVENT));
        });
      };
    };
    connect();
    return () => {
      stopped = true;
      if (reconnectTimerID !== null) { globalThis.clearTimeout(reconnectTimerID); }
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
