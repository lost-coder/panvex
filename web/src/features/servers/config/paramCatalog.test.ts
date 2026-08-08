import { describe, it, expect } from "vitest";
import { PARAM_CATALOG, catalogEntry, PROCESS_OWNED_PATHS } from "./paramCatalog";

describe("paramCatalog loader", () => {
  it("loads a non-trivial number of fields", () => {
    expect(PARAM_CATALOG.length).toBeGreaterThan(150);
  });
  it("resolves an entry by path with a localized description", () => {
    const e = catalogEntry("general.log_level");
    expect(e?.type).toBe("select");
    expect(e?.en.length).toBeGreaterThan(0);
    expect(e?.ru.length).toBeGreaterThan(0);
  });
  it("excludes process-owned paths from the editable catalog", () => {
    for (const p of PROCESS_OWNED_PATHS) expect(catalogEntry(p)).toBeUndefined();
  });
  it("ни один путь каталога не является префиксом другого", () => {
    // Regression guard for the censorship.tls_fetch umbrella-field defect
    // (Task 1 review): an umbrella entry declared e.g. "type": "string" that
    // is a strict path-prefix of its own real children makes unflattenPaths
    // write the parent scalar first, then the next child's setPath sees a
    // scalar mid-path and silently resets it to {} — discarding the parent
    // field's value. Runs against the real generated catalog (not a
    // fixture) so it keeps guarding after future regenerations.
    const paths = PARAM_CATALOG.map((f) => f.path);
    const collisions = paths.filter((p) => paths.some((o) => o.startsWith(`${p}.`)));
    expect(collisions).toEqual([]);
  });
});
