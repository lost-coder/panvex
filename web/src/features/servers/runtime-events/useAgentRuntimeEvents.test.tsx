// Task 2.8 (audit, MEDIUM M13 + H3 hole): regression coverage for the
// runtime-events WS + HTTP decode paths. Before this fix:
//
//   - WS: `JSON.parse(msg.data) as BusMessage` was cast with only an
//     `Array.isArray(data.events)` guard. A `runtime.events` frame with
//     `events: [null]` flowed straight into
//     `.map(record => ({ ts: record.ts, ... }))`, and `null.ts` threw a
//     TypeError inside the synchronous `onmessage` handler — crashing
//     the server-detail live-events section.
//   - HTTP: `api<RuntimeEventsListResponse>(...)` was called with no
//     3rd-arg schema, so a malformed `items` array flowed unchecked
//     into `setEvents(initial.data.items)`.
//
// These tests pin: (1) a malformed WS frame is dropped without
// throwing and without mutating state, (2) a well-formed WS frame
// still applies, (3) a malformed HTTP seed is dropped by the schema
// instead of being cast through.
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import * as React from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/shared/api/api", () => ({
  apiClient: {
    listRuntimeEvents: vi.fn(),
  },
}));

import { apiClient } from "@/shared/api/api";
import { useAgentRuntimeEvents } from "./useAgentRuntimeEvents";

const AGENT_ID = "agent-1";

// Capture the WebSocket instance the hook creates so tests can drive
// `onmessage` directly, mirroring EventsSynchronizer.test.tsx's
// FakeWebSocket harness.
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
  close(): void {
    this.readyState = FakeWebSocket.CLOSED;
  }
}

function wrapper() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return ({ children }: { children: React.ReactNode }) =>
    React.createElement(QueryClientProvider, { client: qc }, children);
}

function sendWsFrame(payload: unknown): void {
  const ws = FakeWebSocket.instances.at(-1);
  expect(ws).toBeDefined();
  ws?.onmessage?.(new MessageEvent("message", { data: JSON.stringify(payload) }));
}

describe("useAgentRuntimeEvents", () => {
  let originalWS: typeof globalThis.WebSocket;

  beforeEach(() => {
    FakeWebSocket.instances = [];
    originalWS = globalThis.WebSocket;
    (globalThis as unknown as { WebSocket: typeof FakeWebSocket }).WebSocket = FakeWebSocket;
    (apiClient.listRuntimeEvents as ReturnType<typeof vi.fn>).mockReturnValue(
      new Promise(() => {}), // never resolves unless a test overrides it
    );
  });

  afterEach(() => {
    (globalThis as unknown as { WebSocket: typeof globalThis.WebSocket }).WebSocket = originalWS;
    vi.restoreAllMocks();
  });

  it("drops a runtime.events frame with a null element (no throw, events stays empty)", () => {
    const { result } = renderHook(() => useAgentRuntimeEvents(AGENT_ID), {
      wrapper: wrapper(),
    });

    expect(() => {
      act(() => {
        sendWsFrame({
          type: "runtime.events",
          data: { agent_id: AGENT_ID, events: [null] },
        });
      });
    }).not.toThrow();

    expect(result.current.events).toEqual([]);
    expect(result.current.isLive).toBe(false);
  });

  it("applies a well-formed runtime.events frame", () => {
    const { result } = renderHook(() => useAgentRuntimeEvents(AGENT_ID), {
      wrapper: wrapper(),
    });

    act(() => {
      sendWsFrame({
        type: "runtime.events",
        data: {
          agent_id: AGENT_ID,
          events: [
            { ts: "2026-07-01T00:00:00Z", level: "warn", message: "hello" },
          ],
        },
      });
    });

    expect(result.current.events).toEqual([
      { ts: "2026-07-01T00:00:00Z", level: "warn", message: "hello" },
    ]);
    expect(result.current.isLive).toBe(true);
  });

  it("ignores frames for a different agent_id", () => {
    const { result } = renderHook(() => useAgentRuntimeEvents(AGENT_ID), {
      wrapper: wrapper(),
    });

    act(() => {
      sendWsFrame({
        type: "runtime.events",
        data: {
          agent_id: "some-other-agent",
          events: [{ ts: "2026-07-01T00:00:00Z", level: "info", message: "nope" }],
        },
      });
    });

    expect(result.current.events).toEqual([]);
  });

  it("drops a non-JSON WS message without throwing", () => {
    const { result } = renderHook(() => useAgentRuntimeEvents(AGENT_ID), {
      wrapper: wrapper(),
    });

    expect(() => {
      const ws = FakeWebSocket.instances.at(-1);
      ws?.onmessage?.(new MessageEvent("message", { data: "not json {{{" }));
    }).not.toThrow();

    expect(result.current.events).toEqual([]);
  });

  it("seeds events from a well-formed HTTP backlog", async () => {
    (apiClient.listRuntimeEvents as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      items: [{ ts: "2026-07-01T00:00:00Z", level: "error", message: "boot failed" }],
    });

    const { result } = renderHook(() => useAgentRuntimeEvents(AGENT_ID), {
      wrapper: wrapper(),
    });

    await waitFor(() => {
      expect(result.current.isLoading).toBe(false);
    });

    expect(result.current.events).toEqual([
      { ts: "2026-07-01T00:00:00Z", level: "error", message: "boot failed" },
    ]);
  });

  it("does not seed events from a malformed HTTP backlog (schema rejects it)", async () => {
    // Simulates what apiClient.listRuntimeEvents would surface once
    // runtime-events.ts validates the response via
    // runtimeEventsListResponseSchema: a malformed payload rejects at
    // the http layer and the query settles into an error state instead
    // of handing the hook an untrusted `items` array.
    (apiClient.listRuntimeEvents as ReturnType<typeof vi.fn>).mockRejectedValueOnce(
      new Error("Response did not match expected schema"),
    );

    const { result } = renderHook(() => useAgentRuntimeEvents(AGENT_ID), {
      wrapper: wrapper(),
    });

    await waitFor(() => {
      expect(result.current.isLoading).toBe(false);
    });

    expect(result.current.events).toEqual([]);
  });
});
