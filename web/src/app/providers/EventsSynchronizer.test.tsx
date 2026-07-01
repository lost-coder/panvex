// Q-10 (audit, HIGH): regression coverage for the WebSocket-message
// decode path. The previous implementation did
// `JSON.parse(message.data) as EventEnvelope` — a runtime cast with no
// validation. These tests pin two contracts:
//
//   1. The new `eventEnvelopeSchema` rejects malformed payloads.
//   2. The synchronizer drops malformed envelopes silently (no React
//      Query invalidation, no state mutation, no exception propagation).
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { eventEnvelopeSchema } from "@/shared/api/schemas/events";
import { AuthContext } from "@/app/providers/AuthProvider";

// The synchronizer only opens a socket once the session is authenticated.
// Tests that exercise the message path therefore render under an
// authenticated AuthContext so a socket exists synchronously on mount.
const AUTHED = {
  user: { id: "u1", username: "admin", role: "admin", totp_enabled: false },
  isLoading: false,
  isAuthenticated: true,
} as const;
const ANON = { user: null, isLoading: false, isAuthenticated: false } as const;

// The synchronizer's `onclose` handler calls apiClient.me() to decide
// whether to reconnect or dispatch SESSION_EXPIRED_EVENT. We never close
// the socket in these tests, but vitest still imports the module — stub
// the API client so no real network call is wired up.
vi.mock("@/shared/api/api", async () => {
  const actual = await vi.importActual<typeof import("@/shared/api/api")>("@/shared/api/api");
  return {
    ...actual,
    apiClient: {
      me: vi.fn().mockResolvedValue({
        id: "u1",
        username: "admin",
        role: "admin",
        totp_enabled: false,
      }),
    },
  };
});

// Capture the WebSocket instance the provider creates so each test can
// drive its `onmessage` handler directly. We don't simulate a full WS
// lifecycle — the provider treats the socket as opaque and we only care
// about the message-decode contract here.
class FakeWebSocket {
  static OPEN = 1;
  static CONNECTING = 0;
  static CLOSED = 3;
  static CLOSING = 2;
  static instances: FakeWebSocket[] = [];
  readyState: number = FakeWebSocket.CONNECTING;
  onopen: ((ev: Event) => void) | null = null;
  onmessage: ((ev: MessageEvent) => void) | null = null;
  onerror: ((ev: Event) => void) | null = null;
  onclose: ((ev: CloseEvent) => void) | null = null;
  url: string;
  constructor(url: string) {
    this.url = url;
    FakeWebSocket.instances.push(this);
  }
  close(): void { this.readyState = FakeWebSocket.CLOSED; }
}

import { EventsSynchronizer } from "./EventsSynchronizer";

describe("eventEnvelopeSchema", () => {
  it("accepts a well-formed envelope with arbitrary data", () => {
    const result = eventEnvelopeSchema.safeParse({
      type: "agents.enrolled",
      data: { agent_id: "a-1" },
    });
    expect(result.success).toBe(true);
    if (result.success) {
      expect(result.data.type).toBe("agents.enrolled");
    }
  });

  it("accepts an envelope where data is null/undefined/scalar", () => {
    expect(eventEnvelopeSchema.safeParse({ type: "x", data: null }).success).toBe(true);
    // missing `data` is fine because z.unknown() accepts anything including absent
    expect(eventEnvelopeSchema.safeParse({ type: "x" }).success).toBe(true);
    expect(eventEnvelopeSchema.safeParse({ type: "x", data: 42 }).success).toBe(true);
  });

  it("rejects a payload missing `type`", () => {
    expect(eventEnvelopeSchema.safeParse({ data: {} }).success).toBe(false);
  });

  it("rejects a payload where `type` is not a string", () => {
    expect(eventEnvelopeSchema.safeParse({ type: 7, data: {} }).success).toBe(false);
    expect(eventEnvelopeSchema.safeParse({ type: null, data: {} }).success).toBe(false);
  });

  it("rejects non-object payloads outright", () => {
    expect(eventEnvelopeSchema.safeParse(null).success).toBe(false);
    expect(eventEnvelopeSchema.safeParse("agents.enrolled").success).toBe(false);
    expect(eventEnvelopeSchema.safeParse(42).success).toBe(false);
  });
});

describe("EventsSynchronizer onmessage decode", () => {
  let originalWS: typeof globalThis.WebSocket;

  beforeEach(() => {
    FakeWebSocket.instances = [];
    originalWS = globalThis.WebSocket;
    // The provider checks `globalThis.window` and `globalThis.location`.
    // jsdom provides both; we just need to override WebSocket.
    (globalThis as unknown as { WebSocket: typeof FakeWebSocket }).WebSocket = FakeWebSocket;
    // jsdom default location.pathname is "/", so the provider's
    // `endsWith("/login")` guard does not bail. We deliberately do not
    // redefine pathname here because jsdom's Location is non-configurable.
  });

  afterEach(() => {
    (globalThis as unknown as { WebSocket: typeof globalThis.WebSocket }).WebSocket = originalWS;
    vi.restoreAllMocks();
  });

  function mount(): { qc: QueryClient } {
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={qc}>
        <AuthContext.Provider value={AUTHED}>
          <EventsSynchronizer>
            <span data-testid="child">ok</span>
          </EventsSynchronizer>
        </AuthContext.Provider>
      </QueryClientProvider>,
    );
    return { qc };
  }

  it("invalidates queries on a well-formed envelope", async () => {
    const { qc } = mount();
    const spy = vi.spyOn(qc, "invalidateQueries").mockResolvedValue();
    const ws = FakeWebSocket.instances.at(-1)!;
    expect(ws).toBeDefined();
    expect(ws.onmessage).toBeTypeOf("function");

    ws.onmessage?.(new MessageEvent("message", {
      data: JSON.stringify({ type: "audit.created", data: {} }),
    }));
    // Yield once for the async applyInvalidation loop.
    await Promise.resolve();
    await Promise.resolve();

    expect(spy).toHaveBeenCalled();
    const firstCallArg = spy.mock.calls[0]?.[0] as { queryKey?: unknown };
    expect(firstCallArg.queryKey).toEqual(["audit"]);
  });

  it("drops a malformed JSON payload without crashing or invalidating", async () => {
    const { qc } = mount();
    const spy = vi.spyOn(qc, "invalidateQueries").mockResolvedValue();
    const ws = FakeWebSocket.instances.at(-1)!;

    expect(() => {
      ws.onmessage?.(new MessageEvent("message", { data: "not json {{{" }));
    }).not.toThrow();
    await Promise.resolve();

    expect(spy).not.toHaveBeenCalled();
  });

  it("drops a payload that parses as JSON but fails the envelope schema", async () => {
    const { qc } = mount();
    const spy = vi.spyOn(qc, "invalidateQueries").mockResolvedValue();
    const ws = FakeWebSocket.instances.at(-1)!;

    // type missing
    ws.onmessage?.(new MessageEvent("message", { data: JSON.stringify({ data: {} }) }));
    // type wrong shape
    ws.onmessage?.(new MessageEvent("message", { data: JSON.stringify({ type: 42, data: {} }) }));
    // top-level array
    ws.onmessage?.(new MessageEvent("message", { data: JSON.stringify(["agents.enrolled"]) }));
    await Promise.resolve();

    expect(spy).not.toHaveBeenCalled();
  });

  it("falls back to a broad refetch when a seq gap is detected", async () => {
    vi.useFakeTimers();
    try {
      const { qc } = mount();
      const spy = vi.spyOn(qc, "invalidateQueries").mockResolvedValue();
      const ws = FakeWebSocket.instances.at(-1)!;

      ws.onmessage?.(new MessageEvent("message", {
        data: JSON.stringify({ type: "audit.created", data: {}, seq: 1 }),
      }));
      await vi.advanceTimersByTimeAsync(0);
      spy.mockClear();

      ws.onmessage?.(new MessageEvent("message", {
        data: JSON.stringify({ type: "audit.created", data: {}, seq: 4 }),
      }));
      // 3.11: the resync is debounced (trailing-edge ~500ms) — it must not
      // fire synchronously.
      expect(spy).not.toHaveBeenCalled();
      await vi.advanceTimersByTimeAsync(500);

      expect(spy).toHaveBeenCalledWith();
    } finally {
      vi.useRealTimers();
    }
  });

  it("coalesces a burst of seq gaps within the debounce window into one resync", async () => {
    vi.useFakeTimers();
    try {
      const { qc } = mount();
      const spy = vi.spyOn(qc, "invalidateQueries").mockResolvedValue();
      const ws = FakeWebSocket.instances.at(-1)!;

      ws.onmessage?.(new MessageEvent("message", {
        data: JSON.stringify({ type: "audit.created", data: {}, seq: 1 }),
      }));
      await vi.advanceTimersByTimeAsync(0);
      spy.mockClear();

      // Three gaps in quick succession, each well inside the 500ms window.
      ws.onmessage?.(new MessageEvent("message", {
        data: JSON.stringify({ type: "audit.created", data: {}, seq: 5 }),
      }));
      await vi.advanceTimersByTimeAsync(100);
      ws.onmessage?.(new MessageEvent("message", {
        data: JSON.stringify({ type: "audit.created", data: {}, seq: 9 }),
      }));
      await vi.advanceTimersByTimeAsync(100);
      ws.onmessage?.(new MessageEvent("message", {
        data: JSON.stringify({ type: "audit.created", data: {}, seq: 13 }),
      }));

      // Still nothing — each new gap re-armed the trailing-edge timer.
      await vi.advanceTimersByTimeAsync(400);
      expect(spy).not.toHaveBeenCalled();

      // Past the final gap's 500ms window, exactly one resync fires.
      await vi.advanceTimersByTimeAsync(100);
      expect(spy).toHaveBeenCalledTimes(1);
      expect(spy).toHaveBeenCalledWith();
    } finally {
      vi.useRealTimers();
    }
  });

  it("keeps per-event invalidation on contiguous seq", async () => {
    const { qc } = mount();
    const spy = vi.spyOn(qc, "invalidateQueries").mockResolvedValue();
    const ws = FakeWebSocket.instances.at(-1)!;

    ws.onmessage?.(new MessageEvent("message", {
      data: JSON.stringify({ type: "audit.created", data: {}, seq: 1 }),
    }));
    ws.onmessage?.(new MessageEvent("message", {
      data: JSON.stringify({ type: "audit.created", data: {}, seq: 2 }),
    }));
    await Promise.resolve();
    await Promise.resolve();

    expect(spy).toHaveBeenCalled();
    for (const call of spy.mock.calls) {
      expect(call.length).toBeGreaterThan(0); // never the broad no-arg form
    }
  });
});

describe("EventsSynchronizer auth gating", () => {
  let originalWS: typeof globalThis.WebSocket;

  beforeEach(() => {
    FakeWebSocket.instances = [];
    originalWS = globalThis.WebSocket;
    (globalThis as unknown as { WebSocket: typeof FakeWebSocket }).WebSocket = FakeWebSocket;
  });

  afterEach(() => {
    (globalThis as unknown as { WebSocket: typeof globalThis.WebSocket }).WebSocket = originalWS;
    vi.restoreAllMocks();
  });

  // Reuse a single QueryClient across rerenders so the ONLY changing effect
  // dependency is auth state — otherwise a fresh QueryClient would itself
  // retrigger the connect/teardown effect and mask the gating behaviour.
  function tree(qc: QueryClient, authValue: typeof AUTHED | typeof ANON) {
    return (
      <QueryClientProvider client={qc}>
        <AuthContext.Provider value={authValue}>
          <EventsSynchronizer>
            <span data-testid="child">ok</span>
          </EventsSynchronizer>
        </AuthContext.Provider>
      </QueryClientProvider>
    );
  }

  it("does NOT open a WebSocket while unauthenticated", () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(tree(qc, ANON));
    expect(FakeWebSocket.instances).toHaveLength(0);
  });

  it("opens a WebSocket once authentication becomes true", () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const { rerender } = render(tree(qc, ANON));
    expect(FakeWebSocket.instances).toHaveLength(0);

    // Simulate the post-login transition: AuthProvider's me() resolves and
    // isAuthenticated flips to true — the synchronizer must connect without
    // requiring a full page reload.
    rerender(tree(qc, AUTHED));
    expect(FakeWebSocket.instances).toHaveLength(1);
  });

  it("closes the socket when authentication is lost", () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const { rerender } = render(tree(qc, AUTHED));
    expect(FakeWebSocket.instances).toHaveLength(1);
    const socket = FakeWebSocket.instances.at(-1)!;
    const closeSpy = vi.spyOn(socket, "close");

    rerender(tree(qc, ANON));
    expect(closeSpy).toHaveBeenCalled();
  });
});
