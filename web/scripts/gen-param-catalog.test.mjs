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
  it("maps an array Type cell (e.g. String[]) to string[], not string", () => {
    const tlsDomains = cat.fields.find((f) => f.path === "censorship.tls_domains");
    expect(tlsDomains.type).toBe("string[]");
  });
  it("does not flip a scalar enum union to an array just because it has bracket-shaped tokens", () => {
    // general.log_level's Type cell is a quoted-literal enum, not an array —
    // regression guard for the array-detection regex added alongside string[].
    expect(cat.fields.find((f) => f.path === "general.log_level").type).toBe("select");
  });
  it("marks Hot-Reload cross as reload", () => {
    expect(cat.fields.find((f) => f.path === "timeouts.client_handshake").applyMode).toBe("reload");
  });
  it("omits default when the doc uses the em-dash placeholder", () => {
    const adTag = cat.fields.find((f) => f.path === "general.ad_tag");
    expect("default" in adTag).toBe(false);
  });
  it("strips the doc's surrounding double-quotes from a string default", () => {
    // general.log_level's Default cell renders as `"normal"` in the markdown —
    // those quotes are markdown formatting, not part of the value.
    const ll = cat.fields.find((f) => f.path === "general.log_level");
    expect(ll.default).toBe("normal");
  });
  it("leaves a non-quoted (numeric) default untouched", () => {
    const ch = cat.fields.find((f) => f.path === "timeouts.client_handshake");
    expect(ch.default).toBe("30");
  });
  it("does not compute a separate key field — entries address by full path only", () => {
    // Regression guard for the key/section split bug: the generator used to
    // additionally emit `key` (the path remainder after the section), which
    // sections.ts then mis-treated as a single nesting level. Entries now
    // carry only path + section; a bare top-level path (no dot) is its own
    // section with no key field at all.
    const dc = cat.fields.find((f) => f.path === "dc_overrides");
    expect(dc.section).toBe("dc_overrides");
    expect(dc).not.toHaveProperty("key");
  });
  it("recovers descriptions after a bracketless dotted sub-heading without losing the section", () => {
    // RU doc has a malformed "# censorship.tls_fetch" (no brackets) where EN has
    // "# [censorship.tls_fetch]". It must populate its own description AND leave
    // the "censorship" section intact so the sibling "## mask" heading right
    // after it still resolves to "censorship.mask", not "censorship.tls_fetch.mask".
    const tlsFetch = cat.fields.find((f) => f.path === "censorship.tls_fetch");
    expect(tlsFetch.ru).toMatch(/TLS-front/);
    const mask = cat.fields.find((f) => f.path === "censorship.mask");
    expect(mask.ru).toMatch(/маскировки/i);
  });
  it("extracts descriptions for h2 headings with a disambiguating parenthetical", () => {
    const ipv4 = cat.fields.find((f) => f.path === "upstreams.ipv4");
    expect(ipv4.en).toMatch(/IPv4/);
    expect(ipv4.ru).toMatch(/IPv4/);
  });
  it("recognizes the Russian 'Top-level keys' heading so bare-key RU descriptions aren't dropped", () => {
    const dc = cat.fields.find((f) => f.path === "dc_overrides");
    expect(dc.ru).toMatch(/переопределяет/i);
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
