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
import { SESSION_EXPIRED_EVENT } from "@/shared/api/http";
import { useAuth } from "@/app/providers/AuthProvider";
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

// 7.1 (аудит #web-1): контекст разнесён. Статус меняется редко и живёт в
// обычном контексте; lastEventAt меняется на КАЖДОЕ событие и живёт во
// внешнем store с подпиской через useSyncExternalStore — ре-рендерятся
// только реальные подписчики таймстампа (useWsUpdateFlash), а не все
// 16+ потребителей useWsStatus().
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

// Default-store: код вне провайдера (тесты, storybook-подобные рендеры)
// получает рабочий, но «немой» store вместо падения на undefined.
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

export function EventsSynchronizer({ children }: Readonly<{ children?: React.ReactNode }>) {
  const queryClient = useQueryClient();
  const { isAuthenticated } = useAuth();
  const [status, setStatus] = useState<WsStatus>("connecting");
  const [reconnectAttempts, setReconnectAttempts] = useState(0);
  // Store создаётся один раз на инстанс провайдера (useState-инициализатор
  // вместо useRef — чистая инициализация без мутаций в рендере).
  const [lastEventStore] = useState<LastEventStore>(createLastEventStore);
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
            lastEventStore.emit(Date.now());
            scheduleFullResync();
            return;
          }
        }
        if (!isKnownEventType(event.type) && console !== undefined) {
          console.debug("events: unknown event type, falling back to broad sweep", event.type);
        }
        // 7.1: вместо setState — emit в store; провайдер и статус-подписчики
        // не ре-рендерятся, подписчики таймстампа получают уведомление.
        lastEventStore.emit(Date.now());
        void applyInvalidation(invalidationsForEvent(event));
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
        {body}
      </WsLastEventContext.Provider>
    </WsStatusContext.Provider>
  );
}
