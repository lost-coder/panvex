import { describe, expect, it } from "vitest";
import { coverageStatus, coverageColor, deployVariant } from "./status";

describe("deployVariant", () => {
  // R10b Task 2 follow-up: the per-deployment status vocabulary is
  // queued/succeeded/failed/awaiting_node. The badge row must read as a
  // coherent escalation — succeeded is green, failed is red, awaiting_node
  // is amber (waiting on an offline node), and queued (a normal in-flight
  // job) stays neutral, distinct from awaiting_node's attention state.
  it("maps succeeded to the ok (green) tone", () => {
    expect(deployVariant("succeeded")).toBe("ok");
  });

  it("maps failed to the error (red) tone", () => {
    expect(deployVariant("failed")).toBe("error");
  });

  it("maps awaiting_node to the warn (amber) tone", () => {
    expect(deployVariant("awaiting_node")).toBe("warn");
  });

  it("leaves queued on the neutral default tone", () => {
    expect(deployVariant("queued")).toBe("default");
  });
});

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
