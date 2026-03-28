// @ts-nocheck
import assert from "node:assert/strict";
import test from "node:test";
import { buildServerDetailViewModel } from "./server-detail-view-model.ts";

function createAgent(overrides = {}) {
  return {
    id: "agent-fra-01",
    node_name: "de-fra-01",
    fleet_group_id: "eu-edge",
    version: "1.14.7",
    read_only: false,
    presence_state: "degraded",
    last_seen_at: "2026-03-24T11:58:00Z",
    runtime: {
      accepting_new_connections: true,
      me_runtime_ready: true,
      me2dc_fallback_enabled: true,
      use_middle_proxy: false,
      startup_status: "ready",
      startup_stage: "steady_state",
      startup_progress_pct: 100,
      initialization_status: "degraded",
      degraded: true,
      initialization_stage: "waiting_for_repair_paths",
      initialization_progress_pct: 86,
      transport_mode: "me_fallback",
      current_connections: 417,
      current_connections_me: 341,
      current_connections_direct: 76,
      active_users: 382,
      connections_total: 21833,
      connections_bad_total: 41,
      handshake_timeouts_total: 7,
      configured_users: 4096,
      dc_coverage_pct: 83,
      healthy_upstreams: 9,
      total_upstreams: 11,
      dcs: [
        {
          dc: 4,
          available_endpoints: 12,
          available_pct: 100,
          required_writers: 10,
          alive_writers: 13,
          coverage_pct: 100,
          rtt_ms: 30,
          load: 0.48,
        },
        {
          dc: -2,
          available_endpoints: 1,
          available_pct: 8,
          required_writers: 3,
          alive_writers: 2,
          coverage_pct: 33,
          rtt_ms: 2129,
          load: 0.93,
        },
        {
          dc: 1,
          available_endpoints: 3,
          available_pct: 25,
          required_writers: 3,
          alive_writers: 4,
          coverage_pct: 67,
          rtt_ms: 137,
          load: 0.71,
        },
      ],
      upstreams: [
        {
          upstream_id: 2,
          route_kind: "primary",
          address: "fra-core-02:443",
          healthy: true,
          fails: 1,
          effective_latency_ms: 13,
        },
        {
          upstream_id: 9,
          route_kind: "fallback",
          address: "ams-relay-02:443",
          healthy: false,
          fails: 19,
          effective_latency_ms: 241,
        },
        {
          upstream_id: 1,
          route_kind: "primary",
          address: "fra-core-01:443",
          healthy: true,
          fails: 0,
          effective_latency_ms: 11,
        },
      ],
      recent_events: [
        {
          sequence: 44,
          timestamp_unix: Math.floor(Date.parse("2026-03-24T11:58:00Z") / 1000),
          event_type: "dc_coverage_offline",
          context: "DC 2 coverage dropped below quorum",
        },
        {
          sequence: 43,
          timestamp_unix: Math.floor(Date.parse("2026-03-24T11:56:00Z") / 1000),
          event_type: "upstream_timeout_warning",
          context: "Fallback upstream ams-relay-02 marked unhealthy",
        },
      ],
    },
    ...overrides,
  };
}

test("buildServerDetailViewModel maps current runtime data into the approved first-slice sections", () => {
  const agent = createAgent();

  const viewModel = buildServerDetailViewModel(agent, {
    nowMs: Date.parse("2026-03-24T12:00:00Z"),
  });

  assert.equal(viewModel.header.nameText, "de-fra-01");
  assert.equal(viewModel.header.statusText, "Degraded");
  assert.equal(viewModel.header.statusTone, "warn");
  assert.equal(viewModel.header.groupText, "eu-edge");
  assert.equal(viewModel.header.versionText, "1.14.7");
  assert.equal(viewModel.header.lastSeenText, "24 Mar 2026, 11:58 UTC");

  assert.equal(viewModel.overviewStats[0]?.label, "Active Users");
  assert.equal(viewModel.overviewStats[0]?.valueText, "382");
  assert.equal(viewModel.overviewStats[1]?.secondaryText, "341 ME, 76 direct");
  assert.equal(viewModel.overviewStats[2]?.valueText, "83%");
  assert.equal(viewModel.overviewStats[3]?.valueText, "9 / 11");
  assert.equal(viewModel.overviewStats[4]?.valueText, "Yes");
  assert.equal(viewModel.overviewStats[5]?.valueText, "Me Fallback");

  assert.equal(viewModel.runtimeProgressCards[0]?.valueText, "Ready");
  assert.equal(viewModel.runtimeProgressCards[1]?.valueText, "Degraded");
  assert.equal(viewModel.runtimeProgressCards[1]?.progressPct, 86);
  assert.equal(viewModel.runtimeFlags[0]?.valueText, "Yes");
  assert.equal(viewModel.runtimeFlags[3]?.valueText, "No");

  assert.equal(viewModel.dcRows[0]?.dcText, "-2");
  assert.equal(viewModel.dcRows[0]?.statusText, "Partial");
  assert.equal(viewModel.dcRows[0]?.coverageText, "33%");
  assert.equal(viewModel.dcRows[1]?.dcText, "1");
  assert.equal(viewModel.dcRows[1]?.statusText, "Partial");
  assert.equal(viewModel.dcRows[2]?.dcText, "4");
  assert.equal(viewModel.dcRows[2]?.statusText, "Healthy");

  assert.equal(viewModel.connectionStats[0]?.valueText, "417");
  assert.equal(viewModel.connectionMeta[0]?.valueText, "21,833");
  assert.equal(viewModel.connectionMeta[1]?.valueText, "41");
  assert.equal(viewModel.connectionMeta[3]?.valueText, "4,096");

  assert.equal(viewModel.upstreamSummaryText, "9 of 11 upstreams healthy");
  assert.equal(viewModel.upstreamRows[0]?.addressText, "ams-relay-02:443");
  assert.equal(viewModel.upstreamRows[0]?.healthText, "Unhealthy");
  assert.equal(viewModel.upstreamRows[1]?.addressText, "fra-core-01:443");

  assert.equal(viewModel.recentEventItems[0]?.text, "DC 2 coverage dropped below quorum");
  assert.equal(viewModel.recentEventItems[0]?.time, "2 min ago");
  assert.equal(viewModel.recentEventItems[0]?.status, "bad");
});

test("buildServerDetailViewModel keeps empty detail sections stable when runtime lists are missing", () => {
  const agent = createAgent({
    presence_state: "offline",
    read_only: true,
    runtime: {
      ...createAgent().runtime,
      degraded: false,
      healthy_upstreams: 0,
      total_upstreams: 0,
      dcs: [],
      upstreams: [],
      recent_events: [],
    },
  });

  const viewModel = buildServerDetailViewModel(agent, {
    nowMs: Date.parse("2026-03-24T12:00:00Z"),
  });

  assert.equal(viewModel.header.statusText, "Offline");
  assert.equal(viewModel.header.statusTone, "bad");
  assert.equal(viewModel.header.readOnlyText, "Read-only");
  assert.equal(viewModel.overviewStats[3]?.secondaryText, "No upstream routes configured");
  assert.equal(viewModel.dcRows.length, 0);
  assert.equal(viewModel.upstreamRows.length, 0);
  assert.equal(viewModel.recentEventItems.length, 0);
  assert.equal(viewModel.upstreamSummaryText, "0 of 0 upstreams healthy");
});

test("buildServerDetailViewModel keeps the header status aligned with backend-reported server state", () => {
  const agent = createAgent({
    presence_state: "online",
    runtime: {
      ...createAgent().runtime,
      degraded: true,
      accepting_new_connections: false,
      dc_coverage_pct: 40,
      healthy_upstreams: 1,
      total_upstreams: 3,
    },
  });

  const viewModel = buildServerDetailViewModel(agent, {
    nowMs: Date.parse("2026-03-24T12:00:00Z"),
  });

  assert.equal(viewModel.header.statusText, "Online");
  assert.equal(viewModel.header.statusTone, "good");
});

test("buildServerDetailViewModel marks explicit failure events as bad even when they contain positive substrings", () => {
  const agent = createAgent({
    runtime: {
      ...createAgent().runtime,
      recent_events: [
        {
          sequence: 45,
          timestamp_unix: Math.floor(Date.parse("2026-03-24T11:59:00Z") / 1000),
          event_type: "connection_failed",
          context: "Edge connection failed on fallback route",
        },
      ],
    },
  });

  const viewModel = buildServerDetailViewModel(agent, {
    nowMs: Date.parse("2026-03-24T12:00:00Z"),
  });

  assert.equal(viewModel.recentEventItems[0]?.status, "bad");
});

test("buildServerDetailViewModel exposes certificate recovery status text for the server header", () => {
  const agent = createAgent({
    certificate_recovery: {
      status: "allowed",
      issued_at_unix: Math.floor(Date.parse("2026-03-24T12:00:00Z") / 1000),
      expires_at_unix: Math.floor(Date.parse("2026-03-24T12:15:00Z") / 1000),
    },
  });

  const viewModel = buildServerDetailViewModel(agent, {
    nowMs: Date.parse("2026-03-24T12:00:00Z"),
  });

  assert.equal(viewModel.header.certificateRecoveryText, "Allowed until 24 Mar 2026, 12:15 UTC");
});

test("buildServerDetailViewModel does not round event age up to the next unit", () => {
  const agent = createAgent({
    runtime: {
      ...createAgent().runtime,
      recent_events: [
        {
          sequence: 46,
          timestamp_unix: Math.floor(Date.parse("2026-03-24T11:00:29Z") / 1000),
          event_type: "upstream_timeout_warning",
          context: "Timeout while probing a remote upstream",
        },
      ],
    },
  });

  const viewModel = buildServerDetailViewModel(agent, {
    nowMs: Date.parse("2026-03-24T12:00:00Z"),
  });

  assert.equal(viewModel.recentEventItems[0]?.time, "59 min ago");
});

test("buildServerDetailViewModel maps me and routing diagnostics from telemetry detail payload", () => {
  const agent = createAgent();

  const viewModel = buildServerDetailViewModel(agent, {
    nowMs: Date.parse("2026-03-24T12:00:00Z"),
    detail: {
      server: {
        agent,
        severity: "warn",
        reason: "Runtime is degraded",
        runtime_freshness: {
          state: "fresh",
          observed_at_unix: Math.floor(Date.parse("2026-03-24T11:58:00Z") / 1000),
        },
        detail_boost: {
          active: true,
          expires_at_unix: Math.floor(Date.parse("2026-03-24T12:10:00Z") / 1000),
          remaining_seconds: 600,
        },
      },
      diagnostics: {
        state: "fresh",
        state_reason: "",
        system_info: {},
        effective_limits: {},
        security_posture: {},
        minimal_all: {
          enabled: true,
          data: {
            network_path: [
              { dc: 2, selected_ip: "149.154.167.40" },
              { dc: 4, selected_ip: "149.154.167.91" },
            ],
          },
        },
        me_pool: {
          enabled: true,
          data: {
            active_generation: 7,
            warm_generation: 8,
            pending_hardswap_generation: 9,
          },
        },
      },
      security_inventory: {
        state: "fresh",
        state_reason: "",
        enabled: true,
        entries_total: 2,
        entries: ["10.0.0.0/24", "192.168.0.0/24"],
      },
    },
  });

  assert.equal(viewModel.meDiagnosticsStateText, "Fresh");
  assert.equal(viewModel.meDiagnosticsRows[0]?.label, "Active Generation");
  assert.equal(viewModel.meDiagnosticsRows[0]?.valueText, "7");
  assert.equal(viewModel.meDiagnosticsRows[1]?.valueText, "8");
  assert.equal(viewModel.meDiagnosticsRows[2]?.valueText, "9");
  assert.equal(viewModel.routingRows[0]?.label, "DC 2 Path");
  assert.equal(viewModel.routingRows[0]?.valueText, "149.154.167.40");
  assert.equal(viewModel.routingRows[1]?.label, "DC 4 Path");
  assert.equal(viewModel.routingRows[1]?.valueText, "149.154.167.91");
});
