import { describe, it, expect } from "vitest";
import { classifyMode } from "./classifyMode";

describe("classifyMode", () => {
  it("returns 'direct' when use_middle_proxy is false", () => {
    expect(classifyMode({ useMiddleProxy: false, meRuntimeReady: false, me2dcFallbackEnabled: false }))
      .toBe("direct");
  });
  it("returns 'me' when ME ready", () => {
    expect(classifyMode({ useMiddleProxy: true, meRuntimeReady: true, me2dcFallbackEnabled: false }))
      .toBe("me");
  });
  it("returns 'fallback' when ME not ready and fallback enabled", () => {
    expect(classifyMode({ useMiddleProxy: true, meRuntimeReady: false, me2dcFallbackEnabled: true }))
      .toBe("fallback");
  });
  it("returns 'me_down' when ME not ready and no fallback", () => {
    expect(classifyMode({ useMiddleProxy: true, meRuntimeReady: false, me2dcFallbackEnabled: false }))
      .toBe("me_down");
  });
});
