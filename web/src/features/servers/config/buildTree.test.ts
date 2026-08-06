import { describe, it, expect } from "vitest";
import { buildTree } from "./buildTree";

describe("buildTree", () => {
  it("groups fields by section and flags drift", () => {
    const desired = { general: { log_level: "normal" } };
    const observed = { general: { log_level: "silent" } };
    const tree = buildTree(desired, observed, new Set());
    const gen = tree.find((s) => s.section === "general")!;
    const ll = gen.fields.find((f) => f.path === "general.log_level")!;
    expect(ll.value).toBe("normal");
    expect(ll.observed).toBe("silent");
    expect(ll.drifted).toBe(true);
  });
  it("marks group-governed paths locked", () => {
    const tree = buildTree({ general: { fast_mode: false } }, { general: { fast_mode: false } },
      new Set(["general.fast_mode"]));
    expect(tree.flatMap((s) => s.fields).find((f) => f.path === "general.fast_mode")!.locked).toBe(true);
  });
  it("flags observed paths with no catalog entry as unknown+readonly", () => {
    const tree = buildTree({}, { general: { mystery_flag: 1 } }, new Set());
    const f = tree.flatMap((s) => s.fields).find((x) => x.path === "general.mystery_flag")!;
    expect(f.unknown).toBe(true);
    expect(f.readonly).toBe(true);
  });
});
