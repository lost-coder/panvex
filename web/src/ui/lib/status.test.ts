import { describe, expect, it } from "vitest";
import { coverageStatus, coverageColor } from "./status";

describe("coverageStatus / coverageColor", () => {
  it("flags coverage below 70% as error", () => {
    expect(coverageStatus(69)).toBe("error");
    expect(coverageColor(69)).toBe("text-status-error");
  });

  it("flags coverage in [70, 100) as warn", () => {
    expect(coverageStatus(70)).toBe("warn");
    expect(coverageStatus(99)).toBe("warn");
    expect(coverageColor(85)).toBe("text-status-warn");
  });

  it("treats full coverage as ok", () => {
    expect(coverageStatus(100)).toBe("ok");
    expect(coverageColor(100)).toBe("text-status-ok");
  });
});
