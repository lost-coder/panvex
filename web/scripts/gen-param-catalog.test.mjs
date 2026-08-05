// @vitest-environment node
import { describe, it, expect } from "vitest";
import { readFileSync } from "node:fs";
import { parseTables, parseEnumOptions, parseDescriptions } from "./gen-param-catalog.mjs";

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
