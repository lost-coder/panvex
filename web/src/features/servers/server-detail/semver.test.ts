import { describe, expect, it } from "vitest";

import { compareSemver, semverLt } from "./semver";

describe("compareSemver", () => {
  it("orders by major, then minor, then patch", () => {
    expect(compareSemver("3.4.10", "3.4.25")).toBe(-1);
    expect(compareSemver("3.4.25", "3.4.10")).toBe(1);
    expect(compareSemver("3.5.0", "3.4.99")).toBe(1);
    expect(compareSemver("4.0.0", "3.99.99")).toBe(1);
    expect(compareSemver("3.4.25", "3.4.25")).toBe(0);
  });

  it("tolerates a leading v", () => {
    expect(compareSemver("v3.4.10", "3.4.25")).toBe(-1);
  });

  it("treats missing/non-numeric segments as 0", () => {
    expect(compareSemver("3.4", "3.4.0")).toBe(0);
    expect(compareSemver("3", "3.0.0")).toBe(0);
    expect(compareSemver("garbage", "0.0.1")).toBe(-1);
  });
});

describe("semverLt", () => {
  it("is true only when a is strictly older", () => {
    expect(semverLt("3.4.10", "3.4.25")).toBe(true);
    expect(semverLt("3.4.25", "3.4.10")).toBe(false);
    expect(semverLt("3.4.25", "3.4.25")).toBe(false);
  });

  it("is false when either side is blank", () => {
    expect(semverLt("", "3.4.25")).toBe(false);
    expect(semverLt("3.4.10", "")).toBe(false);
    expect(semverLt("", "")).toBe(false);
  });
});
