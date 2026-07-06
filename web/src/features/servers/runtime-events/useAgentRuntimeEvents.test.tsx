// 7.4 (audit #web-6): runtime-events feed no longer owns a second
// WebSocket — it subscribes to the panel-wide EventsSynchronizer socket
// via useWsEvents and watches connection status via useWsStatus. The
// harness drives both directly. Retained coverage: a malformed bus frame
// is dropped whole, a well-formed frame applies, a foreign agent_id is
// ignored, the HTTP backlog seeds + schema-rejects, and — new — a
// reconnect (status → "open") refetches the backlog.
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import * as React from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/shared/api/api", () => ({
  apiClient: {
    listRuntimeEvents: vi.fn(),
  },
}));

// 7.4: хук больше не открывает свой WebSocket — он подписывается на
// envelope'ы EventsSynchronizer через useWsEvents и следит за статусом
// через useWsStatus. Тест управляет обоими напрямую.
const listeners = new Set<(envelope: unknown) => void>();
let wsStatus: "connecting" | "open" | "reconnecting" | "closed" = "open";
vi.mock("@/app/providers/EventsSynchronizer", () => ({
  useWsEvents: () => ({
    subscribe: (listener: (envelope: unknown) => void) => {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    },
  }),
  useWsStatus: () => ({ status: wsStatus, reconnectAttempts: 0 }),
}));

import { apiClient } from "@/shared/api/api";
import { useAgentRuntimeEvents } from "./useAgentRuntimeEvents";

const AGENT_ID = "agent-1";

function wrapper() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return ({ children }: { children: React.ReactNode }) =>
    React.createElement(QueryClientProvider, { client: qc }, children);
}

function sendEnvelope(payload: unknown): void {
  act(() => {
    for (const listener of listeners) listener(payload);
  });
}

beforeEach(() => {
  listeners.clear();
  wsStatus = "open";
  (apiClient.listRuntimeEvents as ReturnType<typeof vi.fn>).mockReset();
  (apiClient.listRuntimeEvents as ReturnType<typeof vi.fn>).mockReturnValue(
    new Promise(() => {}), // never resolves unless a test overrides it
  );
});

describe("useAgentRuntimeEvents", () => {
  it("drops a runtime.events frame with a null element (no throw, events stays empty)", () => {
    const { result } = renderHook(() => useAgentRuntimeEvents(AGENT_ID), {
      wrapper: wrapper(),
    });

    expect(() => {
      sendEnvelope({
        type: "runtime.events",
        data: { agent_id: AGENT_ID, events: [null] },
      });
    }).not.toThrow();

    expect(result.current.events).toEqual([]);
    expect(result.current.isLive).toBe(false);
  });

  it("applies a well-formed runtime.events frame", () => {
    const { result } = renderHook(() => useAgentRuntimeEvents(AGENT_ID), {
      wrapper: wrapper(),
    });

    sendEnvelope({
      type: "runtime.events",
      data: {
        agent_id: AGENT_ID,
        events: [
          { ts: "2026-07-01T00:00:00Z", level: "warn", message: "hello" },
        ],
      },
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

    sendEnvelope({
      type: "runtime.events",
      data: {
        agent_id: "some-other-agent",
        events: [{ ts: "2026-07-01T00:00:00Z", level: "info", message: "nope" }],
      },
    });

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

  it("re-fetches the HTTP backlog when the socket comes back (reconnect)", async () => {
    (apiClient.listRuntimeEvents as ReturnType<typeof vi.fn>).mockResolvedValue({
      items: [],
    });
    wsStatus = "reconnecting";
    const { result, rerender } = renderHook(() => useAgentRuntimeEvents(AGENT_ID), {
      wrapper: wrapper(),
    });
    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(apiClient.listRuntimeEvents).toHaveBeenCalledTimes(1);

    wsStatus = "open";
    rerender();

    // Переход не-open → open инвалидирует backlog-query → второй фетч.
    await waitFor(() =>
      expect(apiClient.listRuntimeEvents).toHaveBeenCalledTimes(2),
    );
  });
});
