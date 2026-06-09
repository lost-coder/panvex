import { describe, expect, it } from "vitest";

import { flattenSections, unflattenPaths } from "./sections";

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
