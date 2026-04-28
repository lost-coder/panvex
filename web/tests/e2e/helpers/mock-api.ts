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
  discoveredClients?: Array<Record<string, unknown>>;
  fleet?: Record<string, unknown>;
  fleetGroups?: Array<Record<string, unknown>>;
  appearance?: Record<string, unknown>;
  panel?: Record<string, unknown>;
  updates?: Record<string, unknown>;
  version?: Record<string, unknown>;
  dashboard?: Record<string, unknown>;
  servers?: Record<string, unknown>;
  controlRoom?: Record<string, unknown>;
  jobs?: Array<Record<string, unknown>>;
  audit?: Array<Record<string, unknown>>;
  users?: Array<Record<string, unknown>>;
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

// Fleet response shape — must match fleetSchema in
// src/shared/api/schemas/dashboard.ts. Every counter is required so an
// omission here surfaces as a Zod parse error in the dashboard
// queries.
const DEFAULT_FLEET = {
  total_agents: 0,
  online_agents: 0,
  degraded_agents: 0,
  offline_agents: 0,
  total_instances: 0,
  metric_snapshots: 0,
  live_connections: 0,
  accepting_new_connections_agents: 0,
  middle_proxy_agents: 0,
  dc_issue_agents: 0,
};

// Empty-but-schema-valid /control-room payload. The schema requires
// onboarding + fleet + jobs + recent_activity + recent_runtime_events.
const DEFAULT_CONTROL_ROOM = {
  onboarding: {
    needs_first_server: false,
    setup_complete: true,
    suggested_fleet_group_id: "",
  },
  fleet: DEFAULT_FLEET,
  jobs: { total: 0, queued: 0, running: 0, failed: 0 },
  recent_activity: [],
  recent_runtime_events: [],
};

// Empty-but-schema-valid /telemetry/dashboard payload. Includes the
// recent_events and agent_load_series fields that L-23 / M-10 added
// to the schema.
const DEFAULT_DASHBOARD = {
  fleet: DEFAULT_FLEET,
  attention: [],
  server_cards: [],
  runtime_distribution: {},
  recent_runtime_events: [],
  recent_events: [],
  agent_load_series: [],
};

const json = (route: Route, body: unknown, status = 200) =>
  route.fulfill({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });

// Build a regex that matches a full URL whose pathname starts with the
// given /api/... prefix. The previous "**/api/foo*" glob silently
// matched Vite source files like /src/shared/api/foo.ts, which broke
// the page boot because the mock returned application/json for what
// the browser expected to be a JS module. Anchoring to the pathname
// avoids that whole class of false positives.
function apiPath(prefix: string): RegExp {
  // prefix is e.g. "/api/clients" — match exact, /sub-path, and ?query.
  const escaped = prefix.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  return new RegExp(`^https?://[^/]+${escaped}(/[^?]*)?(\\?.*)?$`);
}

export async function mockApi(page: Page, overrides: ApiMocks = {}) {
  await page.route(apiPath("/api/auth/me"), (route) =>
    json(route, overrides.me ?? DEFAULT_ME),
  );
  await page.route(apiPath("/api/settings/appearance"), (route) => {
    if (route.request().method() === "PUT") {
      return json(route, overrides.appearance ?? DEFAULT_APPEARANCE);
    }
    return json(route, overrides.appearance ?? DEFAULT_APPEARANCE);
  });
  await page.route(apiPath("/api/settings/panel"), (route) =>
    json(route, overrides.panel ?? DEFAULT_PANEL),
  );
  await page.route(apiPath("/api/version"), (route) =>
    json(route, overrides.version ?? DEFAULT_VERSION),
  );
  await page.route(apiPath("/api/clients"), (route) =>
    json(route, overrides.clients ?? []),
  );
  await page.route(apiPath("/api/discovered-clients"), (route) =>
    json(route, overrides.discoveredClients ?? []),
  );
  await page.route(apiPath("/api/agents"), (route) =>
    json(route, overrides.agents ?? []),
  );
  await page.route(apiPath("/api/fleet-groups"), (route) =>
    json(route, overrides.fleetGroups ?? []),
  );
  await page.route(apiPath("/api/jobs"), (route) =>
    json(route, overrides.jobs ?? []),
  );
  await page.route(apiPath("/api/audit"), (route) =>
    json(route, overrides.audit ?? []),
  );
  await page.route(apiPath("/api/users"), (route) =>
    json(route, overrides.users ?? []),
  );
  // Block the events websocket: page.route() does not intercept the
  // upgrade itself but we can refuse the upgrade attempt so the proxy
  // does not log a stream of ECONNREFUSED messages. Tests do not rely
  // on live events; the polling fallback is sufficient.
  await page.route(/\/api\/events/, (route) => route.abort());
  await page.route(apiPath("/api/fleet"), (route) =>
    json(route, overrides.fleet ?? DEFAULT_FLEET),
  );
  await page.route(apiPath("/api/control-room"), (route) =>
    json(route, overrides.controlRoom ?? DEFAULT_CONTROL_ROOM),
  );
  await page.route(apiPath("/api/telemetry/dashboard"), (route) =>
    json(route, overrides.dashboard ?? DEFAULT_DASHBOARD),
  );
  await page.route(apiPath("/api/telemetry/servers"), (route) =>
    json(route, overrides.servers ?? { nodes: [] }),
  );
  await page.route(apiPath("/api/settings/updates"), (route) =>
    json(
      route,
      overrides.updates ?? {
        check_interval_hours: 24,
        auto_update_panel: false,
        auto_update_agents: false,
        github_repo: "",
        github_token: "",
        agent_download_source: "github",
        current_version: "v0.0.0-e2e",
        // state nested matches the Go updateSettingsResponse shape;
        // DashboardContainer reads state.latest_agent_version off this
        // payload so omitting it crashes the dashboard.
        state: {
          latest_panel_version: "",
          latest_agent_version: "",
          panel_changelog: "",
          agent_changelog: "",
          last_checked_at: 0,
        },
      },
    ),
  );
}

export async function mockLoginSuccess(page: Page) {
  await page.route(apiPath("/api/auth/login"), (route) => {
    if (route.request().method() !== "POST") return route.continue();
    return json(route, { status: "ok" });
  });
}

export async function mockLoginFailure(page: Page, code = "invalid_credentials") {
  await page.route(apiPath("/api/auth/login"), (route) => {
    if (route.request().method() !== "POST") return route.continue();
    return json(route, { error: "invalid credentials", code }, 401);
  });
}
