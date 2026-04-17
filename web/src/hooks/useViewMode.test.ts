import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { useViewMode } from "./useViewMode";

describe("useViewMode", () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it("resolves to 'cards' below threshold and 'list' above it when no manual mode", () => {
    const { result } = renderHook(() => useViewMode("clients", 10));
    expect(result.current.resolveMode(5)).toBe("cards");
    expect(result.current.resolveMode(50)).toBe("list");
    expect(result.current.manualMode).toBeNull();
  });

  it("honors manual setMode and persists it to localStorage", () => {
    const { result } = renderHook(() => useViewMode("clients"));
    act(() => result.current.setMode("list"));

    expect(result.current.manualMode).toBe("list");
    expect(localStorage.getItem("panvex-view-mode-clients")).toBe("list");

    // Manual override wins over autoThreshold, even when itemCount is low.
    expect(result.current.resolveMode(1)).toBe("list");
  });

  it("rehydrates from localStorage on mount", () => {
    localStorage.setItem("panvex-view-mode-servers", "cards");
    const { result } = renderHook(() => useViewMode("servers"));
    expect(result.current.manualMode).toBe("cards");
    expect(result.current.resolveMode(999)).toBe("cards");
  });

  it("ignores invalid stored values", () => {
    localStorage.setItem("panvex-view-mode-x", "garbage");
    const { result } = renderHook(() => useViewMode("x"));
    expect(result.current.manualMode).toBeNull();
  });
});
