import { describe, it, expect } from "vitest";

import { CONFIG_FIELDS, fieldsBySection, requiresReload } from "./fieldRegistry";

describe("config field registry", () => {
  it("has no legacy restart mode", () => {
    for (const f of CONFIG_FIELDS) {
      expect(["hot", "reload"]).toContain(f.applyMode);
    }
  });
  it("tags log_level as hot and SNI as reload", () => {
    expect(CONFIG_FIELDS.find((f) => f.path === "general.log_level")?.applyMode).toBe("hot");
    expect(CONFIG_FIELDS.find((f) => f.path === "censorship.tls_domain")?.applyMode).toBe("reload");
  });
  it("groups fields by section", () => {
    const bySection = fieldsBySection();
    expect(bySection.censorship?.some((f) => f.key === "tls_domain")).toBe(true);
  });
  it("requiresReload is true when a reload field changed", () => {
    expect(requiresReload(["censorship.tls_domain"])).toBe(true);
    expect(requiresReload(["general.log_level"])).toBe(false);
  });
  it("requiresReload true iff any changed path is a reload field", () => {
    expect(requiresReload(["general.log_level"])).toBe(false);
    expect(requiresReload(["general.log_level", "censorship.tls_domain"])).toBe(true);
    expect(requiresReload([])).toBe(false);
  });
  it("every field path is section.key and unique", () => {
    const seen = new Set<string>();
    for (const f of CONFIG_FIELDS) {
      expect(f.path).toBe(`${f.section}.${f.key}`);
      expect(seen.has(f.path)).toBe(false);
      seen.add(f.path);
    }
  });
  it("never includes the process-owned fields excluded from editing", () => {
    const processOwned = new Set([
      "general.data_path",
      "general.quota_state_path",
      "general.disable_colors",
    ]);
    for (const f of CONFIG_FIELDS) {
      expect(processOwned.has(f.path)).toBe(false);
    }
  });
});
