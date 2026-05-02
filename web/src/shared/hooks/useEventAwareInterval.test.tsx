import { describe, expect, it, vi } from "vitest";
import { renderHook } from "@testing-library/react";
import { useEventAwareInterval } from "./useEventAwareInterval";

vi.mock("@/app/providers/EventsSynchronizer", () => {
  let status: "open" | "connecting" | "reconnecting" | "closed" = "open";
  return {
    __setStatus: (s: typeof status) => { status = s; },
    useWsStatus: () => ({ status, reconnectAttempts: 0 }),
  };
});
import * as evMod from "@/app/providers/EventsSynchronizer";

describe("useEventAwareInterval", () => {
  it("returns slow interval when ws is open", () => {
    (evMod as unknown as { __setStatus: (s: string) => void }).__setStatus("open");
    const { result } = renderHook(() => useEventAwareInterval(60_000, 15_000));
    expect(result.current).toBe(60_000);
  });

  it("returns fast interval when ws is connecting", () => {
    (evMod as unknown as { __setStatus: (s: string) => void }).__setStatus("connecting");
    const { result } = renderHook(() => useEventAwareInterval(60_000, 15_000));
    expect(result.current).toBe(15_000);
  });

  it("returns fast interval when ws is reconnecting", () => {
    (evMod as unknown as { __setStatus: (s: string) => void }).__setStatus("reconnecting");
    const { result } = renderHook(() => useEventAwareInterval(60_000, 15_000));
    expect(result.current).toBe(15_000);
  });

  it("returns fast interval when ws is closed", () => {
    (evMod as unknown as { __setStatus: (s: string) => void }).__setStatus("closed");
    const { result } = renderHook(() => useEventAwareInterval(60_000, 15_000));
    expect(result.current).toBe(15_000);
  });
});
