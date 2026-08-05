// @vitest-environment node
import { describe, it, expect } from "vitest";
import { readFileSync } from "node:fs";
import { createHash } from "node:crypto";
import { parseTables, parseEnumOptions, parseDescriptions, buildCatalog, fetchAndVerify } from "./gen-param-catalog.mjs";

const en = readFileSync(new URL("./__fixtures__/config-params.en.md", import.meta.url), "utf8");
const ru = readFileSync(new URL("./__fixtures__/config-params.ru.md", import.meta.url), "utf8");

describe("parseTables", () => {
  it("keys rows by section.key with type, default, hot flag", () => {
    const t = parseTables(en);
    expect(t.get("general.log_level")).toMatchObject({ hot: true });
    expect(t.get("general.ad_tag")).toMatchObject({ hot: true });
    expect(t.get("timeouts.client_handshake")).toMatchObject({ default: "30", hot: false });
  });
});

describe("parseEnumOptions", () => {
  it("extracts quoted literal enums", () => {
    expect(parseEnumOptions('`"debug"`, `"verbose"`, `"normal"`, or `"silent"`'))
      .toEqual(["debug", "verbose", "normal", "silent"]);
  });
  it("returns null for non-enum types", () => {
    expect(parseEnumOptions("`u64`")).toBeNull();
  });
});

describe("parseDescriptions", () => {
  it("extracts en description for a key", () => {
    const d = parseDescriptions(en);
    expect(d.get("general.log_level")).toMatch(/verbosity/i);
  });
  it("extracts ru description for a key", () => {
    const d = parseDescriptions(ru);
    expect(d.get("general.log_level")).toMatch(/детализац/i);
  });
});

describe("buildCatalog", () => {
  const cat = buildCatalog(en, ru, "3.4.25");
  it("stamps the version", () => expect(cat.version).toBe("3.4.25"));
  it("only includes editable sections", () => {
    expect(cat.fields.every((f) => ["general","timeouts","censorship","upstreams","dc_overrides"].includes(f.section))).toBe(true);
  });
  it("maps enum type to select with options", () => {
    const ll = cat.fields.find((f) => f.path === "general.log_level");
    expect(ll).toMatchObject({ type: "select", applyMode: "hot", options: ["debug","verbose","normal","silent"] });
    expect(ll.en).toMatch(/verbosity/i);
    expect(ll.ru).toMatch(/детализац/i);
  });
  it("maps u64 to number and bool to boolean", () => {
    expect(cat.fields.find((f) => f.path === "timeouts.client_handshake").type).toBe("number");
    expect(cat.fields.find((f) => f.path === "censorship.mask").type).toBe("boolean");
  });
  it("marks Hot-Reload cross as reload", () => {
    expect(cat.fields.find((f) => f.path === "timeouts.client_handshake").applyMode).toBe("reload");
  });
  it("omits default when the doc uses the em-dash placeholder", () => {
    const adTag = cat.fields.find((f) => f.path === "general.ad_tag");
    expect("default" in adTag).toBe(false);
  });
  it("uses the whole path as key for a bare top-level editable key", () => {
    const dc = cat.fields.find((f) => f.path === "dc_overrides");
    expect(dc.key).toBe("dc_overrides");
  });
});

const sha = (s) => createHash("sha256").update(s).digest("hex");

describe("fetchAndVerify", () => {
  const src = { tag: "3.4.25", repo: "telemt/telemt",
    files: { en: { path: "d/en.md", sha256: sha("EN") }, ru: { path: "d/ru.md", sha256: sha("RU") } } };
  const fakeFetch = (body) => async () => ({ ok: true, text: async () => body });

  it("returns bodies when hashes match", async () => {
    const got = await fetchAndVerify(src, async (url) => ({ ok: true, text: async () => url.includes("en.md") ? "EN" : "RU" }));
    expect(got).toEqual({ en: "EN", ru: "RU" });
  });
  it("throws on hash mismatch", async () => {
    await expect(fetchAndVerify(src, fakeFetch("TAMPERED"))).rejects.toThrow(/sha256 mismatch/);
  });
});
