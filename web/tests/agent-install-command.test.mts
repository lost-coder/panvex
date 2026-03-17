import test from "node:test";
import assert from "node:assert/strict";

import {
  buildInstallCommand,
  buildManualInstallCommand,
  resolvePanelInstallURL
} from "../src/components/agent-install-command.ts";

test("resolvePanelInstallURL uses the configured panel URL from the token when available", () => {
  assert.equal(
    resolvePanelInstallURL("https://panel.example.com/panvex", "https://internal.example.net", "/ignored"),
    "https://panel.example.com/panvex"
  );
});

test("resolvePanelInstallURL falls back to the current origin and root path", () => {
  assert.equal(
    resolvePanelInstallURL("", "https://internal.example.net", "/panvex"),
    "https://internal.example.net/panvex"
  );
});

test("buildInstallCommand embeds the effective panel URL", () => {
  const command = buildInstallCommand("https://panel.example.com/panvex", "token-123", "latest");
  assert.match(command, /--panel-url https:\/\/panel\.example\.com\/panvex/);
});

test("buildManualInstallCommand embeds the effective panel URL", () => {
  const command = buildManualInstallCommand("https://panel.example.com/panvex", "token-123", "v1.2.3");
  assert.match(command, /-panel-url https:\/\/panel\.example\.com\/panvex/);
});
