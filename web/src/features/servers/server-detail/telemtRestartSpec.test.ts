import { describe, it, expect } from "vitest";
import {
  composeRestartSpec,
  isValidRestartName,
  parseRestartSpec,
} from "./telemtRestartSpec";

describe("parseRestartSpec", () => {
  it("splits a named preset spec into preset + name", () => {
    expect(parseRestartSpec("systemd:telemt")).toEqual({
      preset: "systemd",
      name: "telemt",
      command: "",
    });
  });

  it("splits a command spec into preset custom + command", () => {
    expect(parseRestartSpec("command:/usr/local/bin/restart-telemt.sh")).toEqual({
      preset: "custom",
      name: "",
      command: "/usr/local/bin/restart-telemt.sh",
    });
  });

  it("recognises every named preset kind", () => {
    expect(parseRestartSpec("procd:telemt").preset).toBe("procd");
    expect(parseRestartSpec("openrc:telemt").preset).toBe("openrc");
    expect(parseRestartSpec("runit:telemt").preset).toBe("runit");
  });

  it("falls back to custom for an unrecognised kind", () => {
    expect(parseRestartSpec("launchd:telemt")).toEqual({
      preset: "custom",
      name: "",
      command: "launchd:telemt",
    });
  });

  it("falls back to custom for a spec with no ':'", () => {
    expect(parseRestartSpec("telemt")).toEqual({
      preset: "custom",
      name: "",
      command: "telemt",
    });
  });

  it("defaults to systemd/empty for an empty spec", () => {
    expect(parseRestartSpec("")).toEqual({ preset: "systemd", name: "", command: "" });
  });
});

describe("composeRestartSpec", () => {
  it("composes a named preset", () => {
    expect(composeRestartSpec({ preset: "systemd", name: "telemt", command: "" })).toBe(
      "systemd:telemt",
    );
  });

  it("composes a custom command", () => {
    expect(
      composeRestartSpec({ preset: "custom", name: "", command: "/bin/restart.sh --now" }),
    ).toBe("command:/bin/restart.sh --now");
  });

  it("composes to empty string when the name is blank", () => {
    expect(composeRestartSpec({ preset: "systemd", name: "  ", command: "" })).toBe("");
  });

  it("composes to empty string when the custom command is blank", () => {
    expect(composeRestartSpec({ preset: "custom", name: "", command: "  " })).toBe("");
  });

  it("round-trips through parse", () => {
    const spec = "openrc:telemt-proxy";
    expect(composeRestartSpec(parseRestartSpec(spec))).toBe(spec);
  });
});

describe("isValidRestartName", () => {
  it("accepts a plain name", () => {
    expect(isValidRestartName("telemt")).toBe(true);
  });

  it("rejects empty", () => {
    expect(isValidRestartName("  ")).toBe(false);
  });

  it("rejects whitespace inside the name", () => {
    expect(isValidRestartName("tel emt")).toBe(false);
  });

  it("rejects a slash", () => {
    expect(isValidRestartName("etc/telemt")).toBe(false);
  });

  it("rejects '..'", () => {
    expect(isValidRestartName("../telemt")).toBe(false);
  });
});
