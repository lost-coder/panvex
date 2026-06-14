import { describe, expect, it } from "vitest";
import { localizeReason } from "./reason-text";

// Fake translator: echoes the key so we assert the mapping, not the copy.
const t = (key: string) => key;

describe("localizeReason", () => {
  it("maps a known static reason to its key", () => {
    expect(localizeReason("Agent heartbeat is offline", t)).toBe("reason.offline");
  });
  it("maps 'all upstreams down' to its key", () => {
    expect(localizeReason("all upstreams down", t)).toBe("reason.allUpstreamsDown");
  });
  it("handles the dynamic telemt-unreachable prefix, keeping the tail", () => {
    expect(localizeReason("Telemt API unreachable since 5m", t)).toBe("reason.telemtUnreachable 5m");
  });
  it("trims and still matches", () => {
    expect(localizeReason("  Startup is still in progress  ", t)).toBe("reason.startup");
  });
  it("falls back to the raw reason for unknown/composite strings", () => {
    expect(localizeReason("foo bar (on ME→Direct fallback)", t)).toBe("foo bar (on ME→Direct fallback)");
  });
  it("returns empty string unchanged", () => {
    expect(localizeReason("", t)).toBe("");
  });
});
