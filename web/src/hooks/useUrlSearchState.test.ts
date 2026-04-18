import { renderHook, act } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { useUrlSearchState } from "./useUrlSearchState";

describe("useUrlSearchState", () => {
  const initialLocation = window.location.pathname;

  beforeEach(() => {
    window.history.replaceState(null, "", initialLocation);
  });

  afterEach(() => {
    window.history.replaceState(null, "", initialLocation);
  });

  it("returns the fallback when the param is missing", () => {
    const { result } = renderHook(() => useUrlSearchState("q", "all"));
    expect(result.current[0]).toBe("all");
  });

  it("reads an existing URL parameter on mount", () => {
    window.history.replaceState(null, "", `${initialLocation}?q=hello`);
    const { result } = renderHook(() => useUrlSearchState("q", ""));
    expect(result.current[0]).toBe("hello");
  });

  it("writes the value to the URL when set to a non-default", () => {
    const { result } = renderHook(() => useUrlSearchState("status", "all"));
    act(() => result.current[1]("active"));
    expect(new URLSearchParams(window.location.search).get("status")).toBe("active");
    expect(result.current[0]).toBe("active");
  });

  it("removes the param when reverting to the fallback", () => {
    window.history.replaceState(null, "", `${initialLocation}?q=x`);
    const { result } = renderHook(() => useUrlSearchState("q", ""));
    act(() => result.current[1](""));
    expect(window.location.search).toBe("");
  });

  it("syncs with popstate events", () => {
    const { result } = renderHook(() => useUrlSearchState("q", ""));
    act(() => {
      window.history.replaceState(null, "", `${initialLocation}?q=new`);
      window.dispatchEvent(new PopStateEvent("popstate"));
    });
    expect(result.current[0]).toBe("new");
  });
});
