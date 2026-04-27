import { renderHook, act } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { useUrlSearchState } from "./useUrlSearchState";

describe("useUrlSearchState", () => {
  const initialLocation = globalThis.location.pathname;

  beforeEach(() => {
    globalThis.history.replaceState(null, "", initialLocation);
  });

  afterEach(() => {
    globalThis.history.replaceState(null, "", initialLocation);
  });

  it("returns the fallback when the param is missing", () => {
    const { result } = renderHook(() => useUrlSearchState("q", "all"));
    expect(result.current[0]).toBe("all");
  });

  it("reads an existing URL parameter on mount", () => {
    globalThis.history.replaceState(null, "", `${initialLocation}?q=hello`);
    const { result } = renderHook(() => useUrlSearchState("q", ""));
    expect(result.current[0]).toBe("hello");
  });

  it("writes the value to the URL when set to a non-default", () => {
    const { result } = renderHook(() => useUrlSearchState("status", "all"));
    act(() => result.current[1]("active"));
    expect(new URLSearchParams(globalThis.location.search).get("status")).toBe("active");
    expect(result.current[0]).toBe("active");
  });

  it("removes the param when reverting to the fallback", () => {
    globalThis.history.replaceState(null, "", `${initialLocation}?q=x`);
    const { result } = renderHook(() => useUrlSearchState("q", ""));
    act(() => result.current[1](""));
    expect(globalThis.location.search).toBe("");
  });

  it("syncs with popstate events", () => {
    const { result } = renderHook(() => useUrlSearchState("q", ""));
    act(() => {
      globalThis.history.replaceState(null, "", `${initialLocation}?q=new`);
      globalThis.dispatchEvent(new PopStateEvent("popstate"));
    });
    expect(result.current[0]).toBe("new");
  });
});
