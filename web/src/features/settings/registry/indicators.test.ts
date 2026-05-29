import { describe, expect, it } from "vitest";
import type { SchemaEntry, ValuesEntry } from "./types";
import { resolveIndicator } from "./indicators";

function schema(o: Partial<SchemaEntry> = {}): SchemaEntry {
  return { name: "test.field", class: "operational", type: "string", desc: "d", ...o };
}
function values(o: Partial<ValuesEntry> = {}): ValuesEntry {
  return { value: "v", source: "db", locked: false, ...o };
}

describe("resolveIndicator", () => {
  it("returns no indicator for a live, editable field", () => {
    const r = resolveIndicator(schema({ apply: "live" }), values({ apply: "live" }));
    expect(r.kind).toBeNull();
    expect(r.bar).toBeNull();
    expect(r.icon).toBeNull();
  });

  it("returns no indicator when apply is undefined", () => {
    expect(resolveIndicator(schema(), values()).kind).toBeNull();
  });

  it("flags env-override as amber lock (read-only)", () => {
    const r = resolveIndicator(schema(), values({ overridden_by_env: true, locked: true, source: "env", env_var: "PANVEX_X" }));
    expect(r).toMatchObject({ kind: "env-override", bar: "amber", icon: "lock", tone: "amber", spinning: false, tooltipKey: "envOverride" });
  });

  it("flags config_file source as grey lock", () => {
    const r = resolveIndicator(schema(), values({ locked: true, source: "config_file" }));
    expect(r).toMatchObject({ kind: "config-managed", bar: "grey", icon: "lock", tone: "grey", spinning: false, tooltipKey: "configManaged" });
  });

  it("flags bootstrap-class locked field as grey lock", () => {
    const r = resolveIndicator(schema({ class: "bootstrap" }), values({ locked: true, source: "db" }));
    expect(r.kind).toBe("config-managed");
    expect(r.bar).toBe("grey");
  });

  it("flags pending restart as amber spinning restart icon", () => {
    const r = resolveIndicator(schema({ apply: "restart" }), values({ apply: "restart", value: "old", pending_restart: true, pending_value: "new" }));
    expect(r).toMatchObject({ kind: "pending-restart", bar: "amber", icon: "restart", spinning: true, tooltipKey: "pendingRestart" });
  });

  it("does not treat equal pending_value as pending", () => {
    const r = resolveIndicator(schema({ apply: "restart" }), values({ apply: "restart", value: "same", pending_restart: true, pending_value: "same" }));
    expect(r.kind).toBe("needs-restart");
    expect(r.spinning).toBe(false);
  });

  it("flags needs-restart as amber static restart icon", () => {
    const r = resolveIndicator(schema({ apply: "restart" }), values({ apply: "restart" }));
    expect(r).toMatchObject({ kind: "needs-restart", bar: "amber", icon: "restart", spinning: false, tooltipKey: "needsRestart" });
  });

  it("env-override wins over restart tier", () => {
    const r = resolveIndicator(schema({ apply: "restart" }), values({ apply: "restart", overridden_by_env: true, locked: true }));
    expect(r.kind).toBe("env-override");
  });

  it("prefers values.apply over schema.apply", () => {
    const r = resolveIndicator(schema({ apply: "live" }), values({ apply: "restart" }));
    expect(r.kind).toBe("needs-restart");
  });
});
