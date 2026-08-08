import { describe, expect, it } from "vitest";

import { flattenSections, unflattenPaths, getPath, setPath, hasPath } from "./sections";

describe("flattenSections", () => {
  it("flattens only curated CONFIG_FIELDS paths and drops unmanaged fields", () => {
    const flat = flattenSections({
      general: { log_level: "info", update_every: 5, unmanaged: "x" },
      censorship: { tls_domain: "example.com" },
      unmanagedSection: { foo: "bar" },
    });
    expect(flat).toEqual({
      "general.log_level": "info",
      "general.update_every": 5,
      "censorship.tls_domain": "example.com",
    });
    // Unmanaged key inside a managed section is excluded.
    expect(flat).not.toHaveProperty("general.unmanaged");
    // Whole unmanaged section is excluded.
    expect(Object.keys(flat).some((k) => k.startsWith("unmanagedSection"))).toBe(false);
  });

  it("includes only keys actually present in the source sections", () => {
    const flat = flattenSections({ general: { log_level: "debug" } });
    expect(flat).toEqual({ "general.log_level": "debug" });
  });
});

describe("unflattenPaths", () => {
  it("nests curated paths back into a sections object", () => {
    const nested = unflattenPaths({
      "general.log_level": "warn",
      "general.update_every": 10,
      "timeouts.client_handshake": 30,
    });
    expect(nested).toEqual({
      general: { log_level: "warn", update_every: 10 },
      timeouts: { client_handshake: 30 },
    });
  });

  it("ignores non-curated paths", () => {
    const nested = unflattenPaths({
      "general.log_level": "info",
      "general.bogus": "nope",
      "ghost.section": "nope",
    });
    expect(nested).toEqual({ general: { log_level: "info" } });
  });

  it("drops forbidden/unknown sections, keeping only curated paths", () => {
    // Locks the section-allowlist invariant: a path in a non-curated
    // section (e.g. "access") must never round-trip into the PUT body.
    const nested = unflattenPaths({
      "access.users": { x: 1 },
      "censorship.tls_domain": "a",
    });
    expect(nested).toEqual({ censorship: { tls_domain: "a" } });
    expect(nested).not.toHaveProperty("access");
  });

  it("omits empty values so blank overrides are not written", () => {
    const nested = unflattenPaths({
      "general.log_level": "",
      "general.update_every": undefined,
      "censorship.tls_domain": "example.com",
    });
    expect(nested).toEqual({ censorship: { tls_domain: "example.com" } });
  });
});

describe("round-trip", () => {
  it("unflattenPaths(flattenSections(x)) keeps curated fields and drops unmanaged", () => {
    const original = {
      general: { log_level: "info", update_every: 5, unmanaged: "x" },
      censorship: { tls_domain: "a.com", tls_domains: ["b.com", "c.com"] },
      unmanagedSection: { foo: "bar" },
    };
    const round = unflattenPaths(flattenSections(original));
    expect(round).toEqual({
      general: { log_level: "info", update_every: 5 },
      censorship: { tls_domain: "a.com", tls_domains: ["b.com", "c.com"] },
    });
  });
});

describe("вложенные пути каталога", () => {
  it("flattenSections читает лист сквозь вложенную таблицу", () => {
    const flat = flattenSections({
      general: { links: { public_host: "ds87j.metrion.click", public_port: 443 } },
      censorship: { tls_fetch: { strict_route: true } },
    });
    expect(flat["general.links.public_host"]).toBe("ds87j.metrion.click");
    expect(flat["general.links.public_port"]).toBe(443);
    expect(flat["censorship.tls_fetch.strict_route"]).toBe(true);
  });

  it("unflattenPaths строит ВЛОЖЕННУЮ таблицу, а не плоский ключ с точкой", () => {
    const nested = unflattenPaths({
      "general.links.public_host": "new.example.com",
      "general.modes.tls": false,
      "censorship.tls_fetch.strict_route": false,
    });
    expect(nested).toEqual({
      general: { links: { public_host: "new.example.com" }, modes: { tls: false } },
      censorship: { tls_fetch: { strict_route: false } },
    });
    // Ключ-с-точкой — ровно тот дефект, который Telemt молча проглатывает.
    expect(Object.keys(nested.general as object)).not.toContain("links.public_host");
  });

  it("round-trip сохраняет вложенные листья", () => {
    const original = {
      general: { log_level: "debug", links: { show: "*" }, telemetry: { me_level: "normal" } },
    };
    expect(unflattenPaths(flattenSections(original))).toEqual(original);
  });

  it("getPath/setPath/hasPath работают по dotted-пути", () => {
    const obj: Record<string, unknown> = {};
    setPath(obj, "general.links.public_port", 443);
    expect(obj).toEqual({ general: { links: { public_port: 443 } } });
    expect(getPath(obj, "general.links.public_port")).toBe(443);
    expect(hasPath(obj, "general.links.public_port")).toBe(true);
    expect(hasPath(obj, "general.links.public_host")).toBe(false);
    expect(getPath(obj, "general.links.public_host")).toBeUndefined();
  });

  it("getPath не проваливается сквозь скаляр", () => {
    expect(getPath({ general: { log_level: "debug" } }, "general.log_level.nope")).toBeUndefined();
  });
});
