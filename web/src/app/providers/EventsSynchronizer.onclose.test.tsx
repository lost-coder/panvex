// R1.1 (audit 2026-07-07 §1.1): the onclose session probe must only log
// the operator out on a definitive 401. A network failure while the
// panel restarts (self-update is a product feature!) must schedule a
// reconnect and leave the session alone.
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { AuthContext } from "@/app/providers/AuthProvider";
import { ApiError, SESSION_EXPIRED_EVENT } from "@/shared/api/http";
import { authApi } from "@/shared/api/auth";

vi.mock("@/shared/api/auth", () => ({
  authApi: { me: vi.fn() },
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
  close(): void { this.readyState = FakeWebSocket.CLOSED; }
}

import { EventsSynchronizer } from "./EventsSynchronizer";

const AUTHED = {
  user: { id: "u1", username: "admin", role: "admin", totp_enabled: false },
  isLoading: false,
  isAuthenticated: true,
} as const;

describe("EventsSynchronizer onclose session probe", () => {
  let originalWS: typeof globalThis.WebSocket;

  beforeEach(() => {
    FakeWebSocket.instances = [];
    originalWS = globalThis.WebSocket;
    (globalThis as unknown as { WebSocket: typeof FakeWebSocket }).WebSocket =
      FakeWebSocket;
    vi.useFakeTimers();
  });

  afterEach(() => {
    (globalThis as unknown as { WebSocket: typeof globalThis.WebSocket }).WebSocket =
      originalWS;
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  function mount(): void {
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={qc}>
        <AuthContext.Provider value={AUTHED}>
          <EventsSynchronizer />
        </AuthContext.Provider>
      </QueryClientProvider>,
    );
  }

  it("сетевая ошибка probe → reconnect, БЕЗ session-expired", async () => {
    vi.mocked(authApi.me).mockRejectedValue(new TypeError("Failed to fetch"));
    const expired = vi.fn();
    globalThis.addEventListener(SESSION_EXPIRED_EVENT, expired);
    mount();
    expect(FakeWebSocket.instances).toHaveLength(1);

    await act(async () => {
      await FakeWebSocket.instances[0]!.onclose?.(new CloseEvent("close"));
    });
    expect(expired).not.toHaveBeenCalled();

    // backoff первой попытки ≤ 5s — после advance должен появиться
    // новый сокет (reconnect), а не молчание.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5_000);
    });
    expect(FakeWebSocket.instances.length).toBeGreaterThan(1);
    globalThis.removeEventListener(SESSION_EXPIRED_EVENT, expired);
  });

  it("401 при probe → session-expired, БЕЗ reconnect", async () => {
    vi.mocked(authApi.me).mockRejectedValue(
      new ApiError("unauthorized", "unauthorized", 401),
    );
    const expired = vi.fn();
    globalThis.addEventListener(SESSION_EXPIRED_EVENT, expired);
    mount();

    await act(async () => {
      await FakeWebSocket.instances[0]!.onclose?.(new CloseEvent("close"));
    });
    expect(expired).toHaveBeenCalledTimes(1);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(60_000);
    });
    expect(FakeWebSocket.instances).toHaveLength(1);
    globalThis.removeEventListener(SESSION_EXPIRED_EVENT, expired);
  });
});
