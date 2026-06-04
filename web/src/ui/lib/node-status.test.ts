import { describe, expect, it } from "vitest";
import {
  nodeStatePresentation,
  nodeStateFromStatus,
  type NodeState,
} from "./node-status";

describe("nodeStateFromStatus", () => {
  it("maps error → down", () => expect(nodeStateFromStatus("error")).toBe("down"));
  it("maps warn → degraded", () => expect(nodeStateFromStatus("warn")).toBe("degraded"));
  it("maps ok → ok", () => expect(nodeStateFromStatus("ok")).toBe("ok"));
});

describe("nodeStatePresentation", () => {
  it("down is red, glyphed, with the down label key", () => {
    expect(nodeStatePresentation("down")).toEqual({
      tone: "error",
      glyph: "⛔",
      labelKey: "fleet.statusDown",
    });
  });
  it("degraded is amber with the degraded label key", () => {
    expect(nodeStatePresentation("degraded")).toEqual({
      tone: "warn",
      glyph: "▲",
      labelKey: "fleet.statusDegraded",
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
      labelKey: "fleet.statusOffline",
    });
  });
  it("pending is neutral with the pending label key", () => {
    expect(nodeStatePresentation("pending")).toEqual({
      tone: "neutral",
      glyph: "●",
      labelKey: "fleet.statusPending",
    });
  });
  it("covers every NodeState (exhaustive)", () => {
    const all: NodeState[] = ["ok", "degraded", "down", "offline", "pending"];
    for (const s of all) expect(nodeStatePresentation(s).glyph.length).toBeGreaterThan(0);
  });
});
