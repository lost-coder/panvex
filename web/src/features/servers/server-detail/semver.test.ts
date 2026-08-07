import { describe, expect, it } from "vitest";

import { compareSemver, isStrictSemver, semverLt } from "./semver";

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

  it("tolerates a leading v on both sides", () => {
    expect(semverLt("v3.4.10", "v3.4.25")).toBe(true);
    expect(semverLt("v3.4.10", "3.4.25")).toBe(true);
  });

  it("is false when either side is blank", () => {
    expect(semverLt("", "3.4.25")).toBe(false);
    expect(semverLt("3.4.10", "")).toBe(false);
    expect(semverLt("", "")).toBe(false);
  });

  // Strict validation, not coercion: an unparseable version must never be
  // treated as "less than" a real release. Coercing to 0.0.0 would make a
  // dev build or a bare git SHA always show a false "update available"
  // badge, since 0.0.0 < any real release tag.
  it("is false when either side is not a well-formed major.minor.patch tag", () => {
    expect(semverLt("dev", "3.4.25")).toBe(false);
    expect(semverLt("g1a2b3c4", "3.4.25")).toBe(false);
    expect(semverLt("3.4.25", "dev")).toBe(false);
    expect(semverLt("3.4", "3.4.25")).toBe(false);
    expect(semverLt("3", "3.4.25")).toBe(false);
    expect(semverLt("3.4.25-rc1", "3.4.26")).toBe(false);
    expect(semverLt("3.4.25+build.1", "3.4.26")).toBe(false);
  });
});

// Task 4 fix: exported so the version picker can detect an unparseable
// currentVersion up front (forced-install confirm, see
// TelemtUpdateSection.test.tsx).
describe("isStrictSemver", () => {
  it("is true for a well-formed major.minor.patch tag, with or without a leading v", () => {
    expect(isStrictSemver("3.4.25")).toBe(true);
    expect(isStrictSemver("v3.4.25")).toBe(true);
  });

  it("is false for anything else", () => {
    expect(isStrictSemver("")).toBe(false);
    expect(isStrictSemver("dev")).toBe(false);
    expect(isStrictSemver("g1a2b3c4")).toBe(false);
    expect(isStrictSemver("3.4")).toBe(false);
    expect(isStrictSemver("3.4.25-rc1")).toBe(false);
  });
});
