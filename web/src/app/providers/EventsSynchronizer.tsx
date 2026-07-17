import { useQueryClient } from "@tanstack/react-query";
import {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  useSyncExternalStore,
} from "react";

import { authApi } from "@/shared/api/auth";
import { ApiError, SESSION_EXPIRED_EVENT } from "@/shared/api/http";
import { useAuth } from "@/app/providers/AuthProvider";
import { eventEnvelopeSchema, type EventEnvelope } from "@/shared/api/schemas/events";
import { invalidateTelemetryQueries } from "@/shared/events/telemetry-query-invalidation";
import { createCoalescer } from "@/shared/events/invalidation-coalescer";
import { buildEventsURL, resolveConfiguredRootPath } from "@/shared/lib/runtime-path";
import {
  type EventInvalidation,
  invalidationsForEvent,
  isKnownEventType,
} from "@/shared/events/event-invalidations";

// P2-UX-10: expose connection state + "last update" timestamp so the UI can
// surface a reconnection banner and a subtle flash when data arrives via WS.
export type WsStatus = "connecting" | "open" | "reconnecting" | "closed";

// 7.1 (audit #web-1): the context is split apart. Status changes rarely and
// lives in a regular context; lastEventAt changes on EVERY event and lives
// in an external store subscribed to via useSyncExternalStore — only the
// actual timestamp subscribers (useWsUpdateFlash) re-render, not all
// 16+ consumers of useWsStatus().
export interface WsStatusValue {
  status: WsStatus;
  /** Attempt count for the current reconnect loop (0 while healthy). */
  reconnectAttempts: number;
}

interface LastEventStore {
  subscribe: (onChange: () => void) => () => void;
  getSnapshot: () => number | null;
  emit: (ts: number) => void;
}

function createLastEventStore(): LastEventStore {
  let lastEventAt: number | null = null;
  const listeners = new Set<() => void>();
  return {
    subscribe(onChange) {
      listeners.add(onChange);
      return () => {
        listeners.delete(onChange);
      };
    },
    getSnapshot() {
      return lastEventAt;
    },
    emit(ts) {
      lastEventAt = ts;
      for (const listener of listeners) listener();
    },
  };
}

const WsStatusContext = createContext<WsStatusValue>({
  status: "connecting",
  reconnectAttempts: 0,
});

// Default store: code outside the provider (tests, storybook-like renders)
// gets a working but "mute" store instead of crashing on undefined.
const WsLastEventContext = createContext<LastEventStore>(createLastEventStore());

// R-Q-24: provider co-locates the hook by convention; see AppearanceProvider.
// eslint-disable-next-line react-refresh/only-export-components
export function useWsStatus(): WsStatusValue {
  return useContext(WsStatusContext);
}

// eslint-disable-next-line react-refresh/only-export-components
export function useWsLastEventAt(): number | null {
  const store = useContext(WsLastEventContext);
  return useSyncExternalStore(store.subscribe, store.getSnapshot);
}

// 7.4 (#web-6): subscription to valid envelopes for consumers outside
// the invalidation pipeline (today — useAgentRuntimeEvents). One socket
// for the whole panel: /events on the server doesn't support topics
// (http_events.go sends the whole bus to every subscriber), a second socket
// would only duplicate traffic and eat into the per-user connection limit.
export type WsEnvelopeListener = (envelope: EventEnvelope) => void;

export interface WsEventsValue {
  /** Subscribe to all valid envelopes. Returns an unsubscribe function. */
  subscribe: (listener: WsEnvelopeListener) => () => void;
}

const noopSubscribe: WsEventsValue = { subscribe: () => () => {} };
const WsEventsContext = createContext<WsEventsValue>(noopSubscribe);

// eslint-disable-next-line react-refresh/only-export-components
export function useWsEvents(): WsEventsValue {
  return useContext(WsEventsContext);
}

export function EventsSynchronizer({ children }: Readonly<{ children?: React.ReactNode }>) {
  const queryClient = useQueryClient();
  const { isAuthenticated } = useAuth();
  const [status, setStatus] = useState<WsStatus>("connecting");
  const [reconnectAttempts, setReconnectAttempts] = useState(0);
  // The store is created once per provider instance (useState initializer
  // instead of useRef — a clean initialization with no mutations during render).
  const [lastEventStore] = useState<LastEventStore>(createLastEventStore);
  // Envelope subscribers. A ref, not state: broadcasting must not
  // re-render the provider. The context value is stable for the provider's whole lifetime.
  const envelopeListenersRef = useRef<Set<WsEnvelopeListener>>(new Set());
  const [eventsValue] = useState<WsEventsValue>(() => ({
    subscribe: (listener: WsEnvelopeListener) => {
      envelopeListenersRef.current.add(listener);
      return () => {
        envelopeListenersRef.current.delete(listener);
      };
    },
  }));
  // Refs so the long-lived effect doesn't depend on state and re-subscribe.
  const attemptsRef = useRef(0);
  // D6c: last seen hub sequence number for the current socket. null until
  // the first numbered event arrives; reset on every (re)open because the
  // hub counter is panel-global, not per-connection.
  const lastSeqRef = useRef<number | null>(null);

  useEffect(() => {
    if (globalThis.window === undefined) { return; }
    // Gate the socket on authentication. EventsSynchronizer renders on every
    // route (login included), but a session cookie is required for the
    // /api/events upgrade — opening it before login yields a 401, a noisy
    // "disconnected" banner, and a socket that never recovers until a manual
    // reload. With isAuthenticated in the dependency list the effect re-runs
    // when the session is established (post-login) and tears down on logout.
    // The authed branch's cleanup resets `status` to a banner-hidden value,
    // so we don't need to touch it here (and avoid a synchronous setState in
    // the effect body).
    if (!isAuthenticated) { return; }
    let socket: WebSocket | null = null;
    let reconnectDelayMs = 1_000;
    let reconnectTimerID: ReturnType<typeof setTimeout> | null = null;
    let stopped = false;
    // 3.11: a busy fleet can drop several frames in a row, so a burst of
    // seq-gap detections would otherwise fire the same no-arg
    // `invalidateQueries()` (full-cache refetch) once per gap — a refetch
    // storm. Coalesce with a trailing-edge debounce: each gap (re)starts a
    // short timer and only the last one in a ~500ms window actually
    // triggers the resync. Correctness is preserved (a gap still resyncs
    // the whole cache), just collapsed to one call per burst.
    let resyncTimerID: ReturnType<typeof setTimeout> | null = null;
    const scheduleFullResync = () => {
      if (resyncTimerID !== null) { globalThis.clearTimeout(resyncTimerID); }
      resyncTimerID = globalThis.setTimeout(() => {
        resyncTimerID = null;
        void queryClient.invalidateQueries();
      }, 500);
    };
    // 7.4 (#web-8): a storm of same-type events (agents.* across N nodes) used to
    // cause N immediate refetches of the same keys. Keys are accumulated in a
    // Map (dedup by JSON form) and flushed by the coalescer: the first event
    // in a quiet window fires immediately (leading, the operator sees the
    // result of their own action right away), a storm collapses to one refetch on the trailing edge.
    const pendingKeys = new Map<string, readonly unknown[]>();
    let pendingClientDetailSweep = false;
    const keyCoalescer = createCoalescer({
      trailingMs: 2_000,
      maxWaitMs: 10_000,
      leading: true,
    });
    const flushKeyInvalidations = async () => {
      const keys = [...pendingKeys.values()];
      const sweepClientDetails = pendingClientDetailSweep;
      pendingKeys.clear();
      pendingClientDetailSweep = false;
      // Q-10: copy each readonly key into a fresh mutable array for TanStack.
      // Fire them together: the invalidations are independent, and awaiting
      // them one after another made a burst of events serialise refetches.
      await Promise.all(
        keys.map((key) => queryClient.invalidateQueries({ queryKey: [...key] })),
      );
      if (sweepClientDetails) {
        // "clients" sweep also refreshes per-client detail queries
        // (["client", id]) so the detail view updates in-flight.
        await queryClient.invalidateQueries({
          predicate: (query) => query.queryKey[0] === "client",
        });
      }
    };
    const applyInvalidation = (invalidation: EventInvalidation) => {
      for (const key of invalidation.keys) {
        pendingKeys.set(JSON.stringify(key), key);
        if (key[0] === "clients") {
          pendingClientDetailSweep = true;
        }
      }
      if (invalidation.telemetry) {
        invalidateTelemetryQueries(queryClient, invalidation.telemetryAgentID);
      }
      if (pendingKeys.size > 0) {
        keyCoalescer.schedule(() => void flushKeyInvalidations());
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
        // 7.4: delivery to live subscribers (runtime feed, etc.) happens before
        // the seq-gap branch: the current frame is valid, only earlier ones were lost.
        for (const listener of envelopeListenersRef.current) {
          listener(event);
        }
        if (typeof event.seq === "number") {
          const last = lastSeqRef.current;
          lastSeqRef.current = event.seq;
          if (last !== null && event.seq > last + 1) {
            // D6c: the hub dropped frames for this subscriber (slow tab or
            // event burst) — per-event invalidation can no longer be
            // trusted, so refetch everything once and resync.
            lastEventStore.emit(Date.now());
            scheduleFullResync();
            return;
          }
        }
        if (!isKnownEventType(event.type) && console !== undefined) {
          console.debug("events: unknown event type, falling back to broad sweep", event.type);
        }
        // 7.1: instead of setState — emit into the store; the provider and status
        // subscribers don't re-render, timestamp subscribers get notified.
        lastEventStore.emit(Date.now());
        applyInvalidation(invalidationsForEvent(event));
      };
      socket.onerror = () => { socket?.close(); };
      // Q3.U-Q-22: check session validity before reconnecting. On expiration
      // we dispatch SESSION_EXPIRED_EVENT so AuthProvider owns the router
      // navigation + cache clear + toast — instead of a hard
      // globalThis.location.href that bypasses React Query and misses the
      // toast announcement.
      socket.onclose = async () => {
        // Intentional teardown (unmount or logout) sets `stopped` before
        // closing — don't probe the session or schedule a reconnect.
        if (stopped) { return; }
        try {
          await authApi.me();
          scheduleReconnect();
        } catch (err) {
          // Only a definitive 401 proves the session is gone. Any other
          // failure — a network TypeError while the panel restarts
          // (self-update!), a 5xx, a timeout — must NOT log the operator
          // out: the cookie may still be valid, so keep reconnecting
          // (R1.1, audit 2026-07-07 §1.1).
          if (err instanceof ApiError && err.status === 401) {
            stopped = true;
            setStatus("closed");
            globalThis.dispatchEvent(new CustomEvent(SESSION_EXPIRED_EVENT));
          } else {
            scheduleReconnect();
          }
        }
      };
    };
    connect();
    return () => {
      stopped = true;
      keyCoalescer.cancel();
      if (reconnectTimerID !== null) { globalThis.clearTimeout(reconnectTimerID); }
      if (resyncTimerID !== null) { globalThis.clearTimeout(resyncTimerID); }
      if (socket !== null && (socket.readyState === WebSocket.OPEN || socket.readyState === WebSocket.CONNECTING)) {
        socket.close();
      }
      // Reset to a banner-hidden state on teardown (logout / session
      // expiry / unmount) so a later re-login doesn't briefly flash a stale
      // "disconnected" banner before the fresh socket reconnects.
      setStatus("connecting");
    };
  }, [queryClient, isAuthenticated, lastEventStore]);

  const statusValue = useMemo<WsStatusValue>(
    () => ({ status, reconnectAttempts }),
    [status, reconnectAttempts],
  );

  const body = children === undefined ? null : children;
  return (
    <WsStatusContext.Provider value={statusValue}>
      <WsLastEventContext.Provider value={lastEventStore}>
        <WsEventsContext.Provider value={eventsValue}>
          {body}
        </WsEventsContext.Provider>
      </WsLastEventContext.Provider>
    </WsStatusContext.Provider>
  );
}
