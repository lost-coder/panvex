import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { useFallbackEscalation } from "./useFallbackEscalation";

describe("useFallbackEscalation", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("returns inactive state when mode is not fallback", () => {
    const { result } = renderHook(() =>
      useFallbackEscalation("me", 1_700_000_000),
    );
    expect(result.current.active).toBe(false);
    expect(result.current.escalated).toBe(false);
    expect(result.current.durationSeconds).toBe(0);
    expect(result.current.enteredAtUnix).toBeNull();
  });

  it("computes duration relative to enteredAtUnix when active", () => {
    const enteredAtUnix = 1_700_000_000;
    vi.setSystemTime(new Date(enteredAtUnix * 1000 + 5 * 60_000));
    const { result } = renderHook(() =>
      useFallbackEscalation("fallback", enteredAtUnix),
    );
    expect(result.current.active).toBe(true);
    expect(result.current.escalated).toBe(false);
    expect(result.current.durationSeconds).toBe(300);
  });

  it("flips to escalated at the 30-min boundary without a re-render trigger", () => {
    const enteredAtUnix = 1_700_000_000;
    // Start the page 29 minutes after fallback entry.
    vi.setSystemTime(new Date(enteredAtUnix * 1000 + 29 * 60_000));

    const { result } = renderHook(() =>
      useFallbackEscalation("fallback", enteredAtUnix),
    );
    expect(result.current.active).toBe(true);
    expect(result.current.escalated).toBe(false);

    // Advance one minute past the boundary; the hook's setTimeout should
    // fire and bump the tick state, re-deriving escalated=true.
    act(() => {
      vi.setSystemTime(new Date(enteredAtUnix * 1000 + 30 * 60_000 + 1_000));
      vi.advanceTimersByTime(60_000 + 1_000);
    });
    expect(result.current.escalated).toBe(true);
  });

  it("treats already-past-threshold mounts as escalated on next tick", () => {
    const enteredAtUnix = 1_700_000_000;
    // Mount 31 minutes after fallback entry — initial render should
    // already report escalated=true since duration >= threshold.
    vi.setSystemTime(new Date(enteredAtUnix * 1000 + 31 * 60_000));
    const { result } = renderHook(() =>
      useFallbackEscalation("fallback", enteredAtUnix),
    );
    expect(result.current.escalated).toBe(true);
  });

  it("handles null enteredAtUnix by anchoring duration to 0 at mount", () => {
    const now = 1_700_000_000_000; // ms
    vi.setSystemTime(new Date(now));
    const { result } = renderHook(() =>
      useFallbackEscalation("fallback", null),
    );
    expect(result.current.active).toBe(true);
    expect(result.current.durationSeconds).toBe(0);
    expect(result.current.escalated).toBe(false);
    expect(result.current.enteredAtUnix).toBeNull();
  });
});
