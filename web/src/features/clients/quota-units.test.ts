import { describe, expect, it } from "vitest";

import { displayToQuota, quotaToDisplay } from "./quota-units";

describe("quotaToDisplay", () => {
  it("renders 0 / negative / NaN as 0 GB (unlimited)", () => {
    expect(quotaToDisplay(0)).toEqual({ value: 0, unit: "GB" });
    expect(quotaToDisplay(-5)).toEqual({ value: 0, unit: "GB" });
    expect(quotaToDisplay(Number.NaN)).toEqual({ value: 0, unit: "GB" });
  });

  it("picks the largest unit the value reaches", () => {
    expect(quotaToDisplay(10 * 1024 ** 3)).toEqual({ value: 10, unit: "GB" });
    expect(quotaToDisplay(2 * 1024 ** 4)).toEqual({ value: 2, unit: "TB" });
    expect(quotaToDisplay(512 * 1024 ** 2)).toEqual({ value: 512, unit: "MB" });
    expect(quotaToDisplay(1.5 * 1024 ** 3)).toEqual({ value: 1.5, unit: "GB" });
  });

  it("rounds non-round values to 2 decimals", () => {
    expect(quotaToDisplay(1234567890)).toEqual({ value: 1.15, unit: "GB" });
  });

  it("never displays a positive quota as zero", () => {
    expect(quotaToDisplay(1023)).toEqual({ value: 0.01, unit: "MB" });
  });
});

describe("displayToQuota", () => {
  it("round-trips clean values", () => {
    expect(displayToQuota(10, "GB")).toBe(10 * 1024 ** 3);
    expect(displayToQuota(1.5, "GB")).toBe(1.5 * 1024 ** 3);
    expect(displayToQuota(512, "MB")).toBe(512 * 1024 ** 2);
  });

  it("treats NaN / negative / zero as 0 bytes (unlimited)", () => {
    expect(displayToQuota(Number.NaN, "GB")).toBe(0);
    expect(displayToQuota(-1, "TB")).toBe(0);
    expect(displayToQuota(0, "GB")).toBe(0);
  });
});
