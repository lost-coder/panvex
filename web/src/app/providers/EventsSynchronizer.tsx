import { useQueryClient } from "@tanstack/react-query";
import { createContext, useContext, useEffect, useMemo, useRef, useState } from "react";

import { apiClient, SESSION_EXPIRED_EVENT } from "@/shared/api/api";
import { eventEnvelopeSchema } from "@/shared/api/schemas/events";
import { invalidateTelemetryQueries } from "@/shared/events/telemetry-query-invalidation";
import { buildEventsURL, resolveConfiguredRootPath } from "@/shared/lib/runtime-path";
import {
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

export function EventsSynchronizer({ children }: Readonly<{ children?: React.ReactNode }>) {
  const queryClient = useQueryClient();
  const [status, setStatus] = useState<WsStatus>("connecting");
  const [lastEventAt, setLastEventAt] = useState<number | null>(null);
  const [reconnectAttempts, setReconnectAttempts] = useState(0);
  // Refs so the long-lived effect doesn't depend on state and re-subscribe.
  const attemptsRef = useRef(0);
  // D6c: last seen hub sequence number for the current socket. null until
  // the first numbered event arrives; reset on every (re)open because the
  // hub counter is panel-global, not per-connection.
  const lastSeqRef = useRef<number | null>(null);

  useEffect(() => {
    if (globalThis.window === undefined) { return; }
    let socket: WebSocket | null = null;
    let reconnectDelayMs = 1_000;
    let reconnectTimerID: ReturnType<typeof setTimeout> | null = null;
    let stopped = false;
    const applyInvalidation = async (invalidation: EventInvalidation) => {
      for (const key of invalidation.keys) {
        // Q-10: queryKey was previously typed via `key as unknown[]`. The
        // input is `readonly unknown[]` from our own EventInvalidation map
        // (not runtime data), but TanStack expects a mutable `unknown[]` —
        // copy into a fresh array instead of an unchecked cast.
        await queryClient.invalidateQueries({ queryKey: [...key] });
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
        lastSeqRef.current = null;
        reconnectDelayMs = 1_000;
        attemptsRef.current = 0;
        setReconnectAttempts(0);
        setStatus("open");
      };
      socket.onmessage = (message) => {
        // Q-10 (audit, HIGH): runtime-validate the WebSocket payload via
        // Zod before treating it as a typed EventEnvelope. The previous
        // `JSON.parse(...) as EventEnvelope` cast was the same DF-10
        // failure mode the HTTP layer was hardened against — a malformed
        // or hostile payload would flow through with a TS shape it didn't
        // actually have. On parse failure we drop the event silently
        // (DEV-only console.debug) so the synchronizer never crashes.
        let raw: unknown;
        try { raw = JSON.parse(message.data as string); } catch { return; }
        const result = eventEnvelopeSchema.safeParse(raw);
        if (!result.success) {
          // Mirror the ui/sheet dev-flag pattern (see ui/base/sheet.tsx):
          // `import.meta.env.DEV` is Vite's compile-time dev flag; the
          // narrow cast keeps TypeScript happy without a global ambient
          // declaration. Drops out of production builds via constant
          // folding.
          if (
            import.meta !== undefined &&
            (import.meta as ImportMeta & { env?: { DEV?: boolean } }).env?.DEV
          ) {
            console.debug("events: malformed event envelope, dropping", result.error);
          }
          return;
        }
        const event = result.data;
        if (typeof event.seq === "number") {
          const last = lastSeqRef.current;
          lastSeqRef.current = event.seq;
          if (last !== null && event.seq > last + 1) {
            // D6c: the hub dropped frames for this subscriber (slow tab or
            // event burst) — per-event invalidation can no longer be
            // trusted, so refetch everything once and resync.
            setLastEventAt(Date.now());
            void queryClient.invalidateQueries();
            return;
          }
        }
        if (!isKnownEventType(event.type) && console !== undefined) {
          console.debug("events: unknown event type, falling back to broad sweep", event.type);
        }
        setLastEventAt(Date.now());
        void applyInvalidation(invalidationsForEvent(event));
      };
      socket.onerror = () => { socket?.close(); };
      // Q3.U-Q-22: check session validity before reconnecting. On expiration
      // we dispatch SESSION_EXPIRED_EVENT so AuthProvider owns the router
      // navigation + cache clear + toast — instead of a hard
      // globalThis.location.href that bypasses React Query and misses the
      // toast announcement.
      socket.onclose = async () => {
        if (globalThis.location.pathname.endsWith("/login")) {
          stopped = true;
          setStatus("closed");
          return;
        }
        try {
          await apiClient.me();
          scheduleReconnect();
        } catch {
          stopped = true;
          setStatus("closed");
          globalThis.dispatchEvent(new CustomEvent(SESSION_EXPIRED_EVENT));
        }
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
