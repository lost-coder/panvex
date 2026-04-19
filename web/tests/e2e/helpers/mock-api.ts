import type { Page, Route } from "@playwright/test";

/**
 * Canonical API mock helpers for the E2E suite.
 *
 * Each test intercepts `**\/api/**` requests via `page.route()`. Tests
 * compose `mockApi(page, overrides)` to get the baseline happy-path
 * responses and spread their own edge cases on top.
 *
 * Keeping the mocks in one place means a new backend field changes one
 * fixture instead of every test that touches the endpoint. Pair these
 * fixtures with the Zod contract fixtures in
 * src/shared/api/schemas/requests/contracts.test.ts when adding new
 * endpoints.
 */
export interface ApiMocks {
  me?: Record<string, unknown>;
  clients?: Array<Record<string, unknown>>;
  agents?: Array<Record<string, unknown>>;
  fleet?: Record<string, unknown>;
  appearance?: Record<string, unknown>;
  panel?: Record<string, unknown>;
  updates?: Record<string, unknown>;
  version?: Record<string, unknown>;
  dashboard?: Record<string, unknown>;
  servers?: Record<string, unknown>;
}

const DEFAULT_ME = {
  id: "user-1",
  username: "operator",
  role: "admin",
  totp_enabled: false,
};

const DEFAULT_APPEARANCE = {
  theme: "dark",
  density: "comfortable",
  help_mode: "basic",
  updated_at_unix: 0,
};

const DEFAULT_PANEL = {
  http_public_url: "https://panvex.local",
  grpc_public_endpoint: "grpc.panvex.local:8443",
  bootstrap_complete: true,
};

const DEFAULT_VERSION = {
  control_plane: "v0.0.0-e2e",
  commit: "testsha",
  build_time: "2026-04-19T00:00:00Z",
};

const json = (route: Route, body: unknown, status = 200) =>
  route.fulfill({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });

export async function mockApi(page: Page, overrides: ApiMocks = {}) {
  await page.route("**/api/auth/me", (route) =>
    json(route, overrides.me ?? DEFAULT_ME),
  );
  await page.route("**/api/settings/appearance", (route) => {
    if (route.request().method() === "PUT") {
      return json(route, overrides.appearance ?? DEFAULT_APPEARANCE);
    }
    return json(route, overrides.appearance ?? DEFAULT_APPEARANCE);
  });
  await page.route("**/api/settings/panel", (route) =>
    json(route, overrides.panel ?? DEFAULT_PANEL),
  );
  await page.route("**/api/version*", (route) =>
    json(route, overrides.version ?? DEFAULT_VERSION),
  );
  await page.route("**/api/clients*", (route) =>
    json(route, overrides.clients ?? []),
  );
  await page.route("**/api/agents*", (route) =>
    json(route, overrides.agents ?? []),
  );
  await page.route("**/api/fleet*", (route) =>
    json(route, overrides.fleet ?? { total_agents: 0, online_agents: 0, degraded_agents: 0, offline_agents: 0, total_instances: 0, metric_snapshots: 0 }),
  );
  await page.route("**/api/telemetry/dashboard*", (route) =>
    json(route, overrides.dashboard ?? { nodes: [], activity: [] }),
  );
  await page.route("**/api/telemetry/servers*", (route) =>
    json(route, overrides.servers ?? { nodes: [] }),
  );
  await page.route("**/api/settings/updates*", (route) =>
    json(route, overrides.updates ?? { check_interval_hours: 24, auto_update_panel: false, auto_update_agents: false, agent_download_source: "github" }),
  );
}

export async function mockLoginSuccess(page: Page) {
  await page.route("**/api/auth/login", (route) => {
    if (route.request().method() !== "POST") return route.continue();
    return json(route, { status: "ok" });
  });
}

export async function mockLoginFailure(page: Page, code = "invalid_credentials") {
  await page.route("**/api/auth/login", (route) => {
    if (route.request().method() !== "POST") return route.continue();
    return json(route, { error: "invalid credentials", code }, 401);
  });
}
