import { describe, expect, it } from "vitest";
import {
  nodeStatePresentation,
  deriveNodeState,
  isStartupReason,
  type NodeState,
} from "./node-status";

describe("nodeStatePresentation", () => {
  it("down is red, glyphed, with the down label key", () => {
    expect(nodeStatePresentation("down")).toEqual({
      tone: "error",
      glyph: "⛔",
      labelKey: "status.down",
    });
  });
  it("degraded is amber with the degraded label key", () => {
    expect(nodeStatePresentation("degraded")).toEqual({
      tone: "warn",
      glyph: "▲",
      labelKey: "status.degraded",
    });
  });
  it("ok is green with a check glyph", () => {
    const p = nodeStatePresentation("ok");
    expect(p.tone).toBe("ok");
    expect(p.glyph).toBe("✓");
  });
  it("offline is red with the offline label key", () => {
    expect(nodeStatePresentation("offline")).toEqual({
      tone: "error",
      glyph: "⛔",
      labelKey: "status.offline",
    });
  });
  it("pending is neutral with the pending label key", () => {
    expect(nodeStatePresentation("pending")).toEqual({
      tone: "neutral",
      glyph: "●",
      labelKey: "status.pending",
    });
  });
  it("covers every NodeState (exhaustive)", () => {
    const all: NodeState[] = ["ok", "degraded", "down", "offline", "pending"];
    for (const s of all) expect(nodeStatePresentation(s).glyph.length).toBeGreaterThan(0);
  });
});

describe("deriveNodeState", () => {
  const base = {
    severity: "ok" as const,
    presenceState: "online",
    telemtUnreachable: false,
    reason: "",
  };
  it("offline presence wins over everything", () => {
    expect(deriveNodeState({ ...base, presenceState: "offline", severity: "critical" })).toBe("offline");
  });
  it("telemt unreachable → down", () => {
    expect(deriveNodeState({ ...base, telemtUnreachable: true })).toBe("down");
  });
  it("critical severity → down", () => {
    expect(deriveNodeState({ ...base, severity: "critical" })).toBe("down");
  });
  it("bad severity → down", () => {
    expect(deriveNodeState({ ...base, severity: "bad" })).toBe("down");
  });
  it("startup reason → pending (even though severity is warn)", () => {
    expect(deriveNodeState({ ...base, severity: "warn", reason: "Startup is still in progress" })).toBe("pending");
  });
  it("warn severity (non-startup) → degraded", () => {
    expect(deriveNodeState({ ...base, severity: "warn", reason: "DC coverage is degraded" })).toBe("degraded");
  });
  it("healthy → ok", () => {
    expect(deriveNodeState(base)).toBe("ok");
    expect(deriveNodeState({ ...base, severity: "good" })).toBe("ok");
  });
});

describe("isStartupReason (backend contract)", () => {
  // CONTRACT: this literal must match internal/controlplane/telemetry/projections.go
  // SeverityAndReason → the "Startup is still in progress" return. If the Go
  // literal changes, update STARTUP_REASONS and this test together.
  it("recognizes the exact backend startup reason", () => {
    expect(isStartupReason("Startup is still in progress")).toBe(true);
  });
  it("trims surrounding whitespace", () => {
    expect(isStartupReason("  Startup is still in progress  ")).toBe(true);
  });
  it("rejects unrelated reasons", () => {
    expect(isStartupReason("DC coverage is degraded")).toBe(false);
    expect(isStartupReason("")).toBe(false);
  });
});
