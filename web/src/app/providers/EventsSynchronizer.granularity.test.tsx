// 7.1 (аудит #web-1): WS-событие НЕ должно ре-рендерить компонент,
// подписанный только на status. lastEventAt уходит из контекста во
// внешний store (useSyncExternalStore) — ре-рендерятся только его
// подписчики.
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { AuthContext } from "@/app/providers/AuthProvider";

const AUTHED = {
  user: { id: "u1", username: "admin", role: "admin", totp_enabled: false },
  isLoading: false,
  isAuthenticated: true,
} as const;

vi.mock("@/shared/api/auth", () => ({
  authApi: {
    me: vi.fn().mockResolvedValue({
      id: "u1",
      username: "admin",
      role: "admin",
      totp_enabled: false,
    }),
  },
}));

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

import {
  EventsSynchronizer,
  useWsLastEventAt,
  useWsStatus,
} from "./EventsSynchronizer";

let statusRenders = 0;
function StatusProbe() {
  statusRenders++;
  const { status } = useWsStatus();
  return <div data-testid="status">{status}</div>;
}

let tsRenders = 0;
function TsProbe() {
  tsRenders++;
  const ts = useWsLastEventAt();
  return <div data-testid="ts">{String(ts)}</div>;
}

describe("EventsSynchronizer granularity (7.1)", () => {
  let originalWS: typeof globalThis.WebSocket;

  beforeEach(() => {
    FakeWebSocket.instances = [];
    statusRenders = 0;
    tsRenders = 0;
    originalWS = globalThis.WebSocket;
    (globalThis as unknown as { WebSocket: typeof FakeWebSocket }).WebSocket =
      FakeWebSocket;
  });

  afterEach(() => {
    (globalThis as unknown as { WebSocket: typeof globalThis.WebSocket }).WebSocket =
      originalWS;
  });

  function renderProbes() {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={qc}>
        <AuthContext.Provider value={AUTHED}>
          <EventsSynchronizer>
            <StatusProbe />
            <TsProbe />
          </EventsSynchronizer>
        </AuthContext.Provider>
      </QueryClientProvider>,
    );
  }

  it("WS-событие не ре-рендерит компонент, подписанный только на status", () => {
    renderProbes();
    const ws = FakeWebSocket.instances.at(-1);
    expect(ws).toBeDefined();
    act(() => {
      ws!.readyState = FakeWebSocket.OPEN;
      ws!.onopen?.(new Event("open"));
    });
    expect(screen.getByTestId("status").textContent).toBe("open");

    const statusBefore = statusRenders;
    const tsBefore = tsRenders;
    act(() => {
      ws!.onmessage?.(
        new MessageEvent("message", {
          data: JSON.stringify({
            type: "agents.updated",
            data: { agent_id: "a1" },
            seq: 1,
          }),
        }),
      );
    });

    // Подписчик lastEventAt ре-рендерится, подписчик status — нет.
    expect(tsRenders).toBeGreaterThan(tsBefore);
    expect(statusRenders).toBe(statusBefore);
    expect(screen.getByTestId("ts").textContent).not.toBe("null");
  });

  it("смена статуса ре-рендерит status-подписчика", () => {
    renderProbes();
    const ws = FakeWebSocket.instances.at(-1);
    act(() => {
      ws!.readyState = FakeWebSocket.OPEN;
      ws!.onopen?.(new Event("open"));
    });
    expect(screen.getByTestId("status").textContent).toBe("open");
  });
});
