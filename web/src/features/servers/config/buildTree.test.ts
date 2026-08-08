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
  it("does not flag array fields as drifted when contents are equal but instances differ", () => {
    const desired = { censorship: { tls_domains: ["a", "b"] } };
    const observed = { censorship: { tls_domains: ["a", "b"] } };
    const tree = buildTree(desired, observed, new Set());
    const f = tree.flatMap((s) => s.fields).find((x) => x.path === "censorship.tls_domains")!;
    expect(desired.censorship.tls_domains).not.toBe(observed.censorship.tls_domains);
    expect(f.drifted).toBe(false);
  });
  it("flags array fields as drifted when contents genuinely differ", () => {
    const desired = { censorship: { tls_domains: ["a"] } };
    const observed = { censorship: { tls_domains: ["a", "b"] } };
    const tree = buildTree(desired, observed, new Set());
    const f = tree.flatMap((s) => s.fields).find((x) => x.path === "censorship.tls_domains")!;
    expect(f.drifted).toBe(true);
  });

  it("помечает present=false для пути, которого нет ни в desired, ни в observed", () => {
    const sections = buildTree({}, { general: { fast_mode: true } }, new Set());
    const fields = sections.flatMap((s) => s.fields);

    const known = fields.find((f) => f.path === "general.fast_mode");
    expect(known?.present).toBe(true);
    expect(known?.value).toBe(true);

    // proxy_config_v4_url есть в каталоге, но Telemt его не сериализовал.
    const absent = fields.find((f) => f.path === "general.proxy_config_v4_url");
    expect(absent?.present).toBe(false);
    expect(absent?.value).toBeUndefined();
  });

  it("present=true для поля, заданного только в desired", () => {
    const sections = buildTree({ general: { log_level: "debug" } }, {}, new Set());
    const field = sections.flatMap((s) => s.fields).find((f) => f.path === "general.log_level");
    expect(field?.present).toBe(true);
  });
});
