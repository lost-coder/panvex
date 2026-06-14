import { describe, expect, it } from "vitest";
import {
  invalidationsForEvent,
  isKnownEventType,
} from "./event-invalidations";

describe("invalidationsForEvent", () => {
  it("invalidates control-room and agents for every agents.* event", () => {
    const result = invalidationsForEvent({
      type: "agents.enrolled",
      data: { agent_id: "agent-1" },
    });
    expect(result.keys).toEqual([["control-room"], ["agents"]]);
    expect(result.telemetry).toBe(true);
    expect(result.telemetryAgentID).toBe("agent-1");
  });

  it("falls back gracefully when agent id is missing or non-string", () => {
    const result = invalidationsForEvent({
      type: "agents.updated",
      data: { unrelated: 42 },
    });
    expect(result.telemetry).toBe(true);
    expect(result.telemetryAgentID).toBeUndefined();
  });

  it("dispatches jobs.* to jobs + control-room only, no telemetry", () => {
    const result = invalidationsForEvent({
      type: "jobs.completed",
      data: {},
    });
    expect(result.keys).toEqual([["jobs"], ["control-room"]]);
    expect(result.telemetry).toBeUndefined();
  });

  it("dispatches audit.created to audit only", () => {
    const result = invalidationsForEvent({ type: "audit.created", data: {} });
    expect(result.keys).toEqual([["audit"]]);
  });

  it("dispatches clients.* to clients + control-room", () => {
    const result = invalidationsForEvent({ type: "clients.updated", data: {} });
    expect(result.keys).toEqual([["clients"], ["control-room"]]);
  });

  it("falls back to a broad sweep on unknown event types", () => {
    const result = invalidationsForEvent({ type: "mystery.event", data: {} });
    expect(result.keys).toEqual([
      ["control-room"],
      ["agents"],
      ["clients"],
      ["audit"],
      ["jobs"],
    ]);
    expect(result.telemetry).toBe(true);
  });

  it("returns no invalidations for runtime.events (handled by the per-agent hook)", () => {
    const result = invalidationsForEvent({ type: "runtime.events", data: { agent_id: "a-1" } });
    expect(result.keys).toEqual([]);
    expect(result.telemetry).toBeUndefined();
  });
});

describe("isKnownEventType", () => {
  it("recognises the canonical event families", () => {
    expect(isKnownEventType("agents.enrolled")).toBe(true);
    expect(isKnownEventType("jobs.created")).toBe(true);
    expect(isKnownEventType("audit.created")).toBe(true);
    expect(isKnownEventType("clients.updated")).toBe(true);
  });

  it("treats runtime.events as a known type (no broad-sweep fallback)", () => {
    expect(isKnownEventType("runtime.events")).toBe(true);
  });

  it("returns false for unknown types", () => {
    expect(isKnownEventType("audit.rotated")).toBe(false);
    expect(isKnownEventType("mystery")).toBe(false);
    expect(isKnownEventType("")).toBe(false);
  });
});
