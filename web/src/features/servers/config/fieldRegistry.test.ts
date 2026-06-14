import { describe, it, expect } from "vitest";
import { CONFIG_FIELDS, fieldsBySection, requiresRestart } from "./fieldRegistry";

describe("config field registry", () => {
  it("tags log_level as hot and SNI as restart", () => {
    expect(CONFIG_FIELDS.find((f) => f.path === "general.log_level")?.applyMode).toBe("hot");
    expect(CONFIG_FIELDS.find((f) => f.path === "censorship.tls_domain")?.applyMode).toBe("restart");
  });
  it("groups fields by section", () => {
    const bySection = fieldsBySection();
    expect(bySection.censorship?.some((f) => f.key === "tls_domain")).toBe(true);
  });
  it("requiresRestart true iff any changed path is a restart field", () => {
    expect(requiresRestart(["general.log_level"])).toBe(false);
    expect(requiresRestart(["general.log_level", "censorship.tls_domain"])).toBe(true);
    expect(requiresRestart([])).toBe(false);
  });
  it("every field path is section.key and unique", () => {
    const seen = new Set<string>();
    for (const f of CONFIG_FIELDS) {
      expect(f.path).toBe(`${f.section}.${f.key}`);
      expect(seen.has(f.path)).toBe(false);
      seen.add(f.path);
    }
  });
});
