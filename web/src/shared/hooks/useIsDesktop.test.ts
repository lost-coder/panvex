import { renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { useIsDesktop } from "./useIsDesktop";

describe("useIsDesktop", () => {
  it("defaults to desktop when matchMedia is unavailable (jsdom/SSR)", () => {
    // jsdom does not implement window.matchMedia; the guarded snapshot must
    // fall back to the desktop default instead of throwing.
    const { result } = renderHook(() => useIsDesktop());
    expect(result.current).toBe(true);
  });
});
