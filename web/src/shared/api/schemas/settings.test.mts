import assert from "node:assert/strict";
import test from "node:test";

import { panelSettingsResponseSchema } from "./settings.ts";

const BASE = {
  http_public_url: "https://panel.example",
  http_root_path: "/",
  grpc_public_endpoint: "agents.example:8443",
  http_listen_address: "0.0.0.0:8080",
  grpc_listen_address: "0.0.0.0:8443",
  tls_mode: "proxy" as const,
  tls_cert_file: "",
  tls_key_file: "",
  runtime_source: "legacy" as const,
  runtime_config_path: "",
  updated_at_unix: 1700000000,
  restart: { supported: true, pending: false, state: "ready" as const },
};

test("panelSettingsResponseSchema accepts a valid payload with password_min_length", () => {
  const ok = panelSettingsResponseSchema.parse({ ...BASE, password_min_length: 14 });
  assert.equal(ok.password_min_length, 14);
});

test("panelSettingsResponseSchema rejects password_min_length below 8", () => {
  const result = panelSettingsResponseSchema.safeParse({ ...BASE, password_min_length: 4 });
  assert.equal(result.success, false);
});

test("panelSettingsResponseSchema rejects password_min_length above 64", () => {
  const result = panelSettingsResponseSchema.safeParse({ ...BASE, password_min_length: 65 });
  assert.equal(result.success, false);
});

test("panelSettingsResponseSchema rejects non-integer password_min_length", () => {
  const result = panelSettingsResponseSchema.safeParse({ ...BASE, password_min_length: 12.5 });
  assert.equal(result.success, false);
});
