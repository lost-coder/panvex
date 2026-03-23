import test from "node:test";
import assert from "node:assert/strict";

import {
  buildFleetDcCoverageSummary,
  buildFleetKpiSummary,
  buildServerCardDetails,
  buildServerCardSummary,
  extractRecentRuntimeEvents,
  sortAgentsBySeverity,
  sumFleetTraffic,
} from "./dashboard-view-model.ts";

test("sumFleetTraffic adds client traffic totals", () => {
  const total = sumFleetTraffic([
    { traffic_used_bytes: 125, enabled: true } as any,
    { traffic_used_bytes: 875, enabled: false } as any,
  ]);

  assert.equal(total, 1000);
});

test("sortAgentsBySeverity orders offline before degraded before online", () => {
  const agents = sortAgentsBySeverity([
    {
      id: "online",
      presence_state: "online",
      runtime: { degraded: false, accepting_new_connections: true, dc_coverage_pct: 100, healthy_upstreams: 2, total_upstreams: 2 },
    } as any,
    {
      id: "degraded",
      presence_state: "online",
      runtime: { degraded: true, accepting_new_connections: true, dc_coverage_pct: 100, healthy_upstreams: 2, total_upstreams: 2 },
    } as any,
    {
      id: "offline",
      presence_state: "offline",
      runtime: { degraded: false, accepting_new_connections: true, dc_coverage_pct: 100, healthy_upstreams: 2, total_upstreams: 2 },
    } as any,
  ]);

  assert.deepEqual(agents.map((agent) => agent.id), ["offline", "degraded", "online"]);
});

test("sortAgentsBySeverity treats degraded presence as degraded", () => {
  const agents = sortAgentsBySeverity([
    {
      id: "healthy",
      node_name: "healthy",
      presence_state: "online",
      runtime: { degraded: false, accepting_new_connections: true, dc_coverage_pct: 100, healthy_upstreams: 2, total_upstreams: 2 },
    } as any,
    {
      id: "presence-degraded",
      node_name: "presence-degraded",
      presence_state: "degraded",
      runtime: { degraded: false, accepting_new_connections: true, dc_coverage_pct: 100, healthy_upstreams: 2, total_upstreams: 2 },
    } as any,
  ]);

  assert.deepEqual(agents.map((agent) => agent.id), ["presence-degraded", "healthy"]);
});

test("extractRecentRuntimeEvents sorts latest runtime events first", () => {
  const events = extractRecentRuntimeEvents([
    {
      id: "server-a",
      node_name: "alpha",
      runtime: {
        recent_events: [
          { sequence: 1, timestamp_unix: 100, event_type: "connect", context: "old" },
        ],
      },
    } as any,
    {
      id: "server-b",
      node_name: "beta",
      runtime: {
        recent_events: [
          { sequence: 2, timestamp_unix: 200, event_type: "error", context: "new" },
        ],
      },
    } as any,
  ]);

  assert.equal(events[0]?.agentName, "beta");
  assert.equal(events[0]?.timestampUnix, 200);
  assert.equal(events[1]?.agentName, "alpha");
});

test("buildFleetDcCoverageSummary aggregates dc health", () => {
  const summary = buildFleetDcCoverageSummary([
    {
      runtime: {
        dcs: [
          { dc: 1, available_endpoints: 2, available_pct: 100, required_writers: 3, alive_writers: 3, coverage_pct: 100, rtt_ms: 10, load: 1 },
          { dc: 2, available_endpoints: 1, available_pct: 66, required_writers: 3, alive_writers: 2, coverage_pct: 66, rtt_ms: 120, load: 2 },
        ],
      },
    } as any,
    {
      runtime: {
        dcs: [
          { dc: 2, available_endpoints: 1, available_pct: 33, required_writers: 3, alive_writers: 1, coverage_pct: 33, rtt_ms: 220, load: 3 },
        ],
      },
    } as any,
  ]);

  assert.equal(summary.totalDcCount, 2);
  assert.equal(summary.okCount, 1);
  assert.equal(summary.partialCount, 1);
  assert.equal(summary.downCount, 0);
  assert.equal(summary.rows[1]?.dc, 2);
  assert.equal(summary.rows[1]?.health, "partial");
});

test("buildFleetDcCoverageSummary surfaces down dc coverage when any server is down", () => {
  const summary = buildFleetDcCoverageSummary([
    {
      runtime: {
        dcs: [
          { dc: 7, available_endpoints: 2, available_pct: 100, required_writers: 3, alive_writers: 3, coverage_pct: 100, rtt_ms: 10, load: 1 },
        ],
      },
    } as any,
    {
      runtime: {
        dcs: [
          { dc: 7, available_endpoints: 0, available_pct: 0, required_writers: 3, alive_writers: 0, coverage_pct: 0, rtt_ms: 400, load: 2 },
        ],
      },
    } as any,
  ]);

  assert.equal(summary.totalDcCount, 1);
  assert.equal(summary.downCount, 1);
  assert.equal(summary.rows[0]?.dc, 7);
  assert.equal(summary.rows[0]?.health, "down");
});

test("buildServerCardSummary keeps unavailable slots as dashes", () => {
  const summary = buildServerCardSummary({
    id: "server-1",
    node_name: "node-1",
    presence_state: "online",
    fleet_group_id: "group-a",
    version: "1.2.3",
    read_only: false,
    runtime: {
      me_runtime_ready: true,
      accepting_new_connections: true,
      degraded: false,
      current_connections: 12,
      current_connections_me: 7,
      current_connections_direct: 5,
      active_users: 4,
      connections_total: 0,
      connections_bad_total: 0,
      handshake_timeouts_total: 0,
      configured_users: 0,
      dc_coverage_pct: 100,
      healthy_upstreams: 3,
      total_upstreams: 3,
      use_middle_proxy: false,
      me2dc_fallback_enabled: false,
      startup_status: "ready",
      startup_stage: "ready",
      startup_progress_pct: 100,
      initialization_status: "ready",
      initialization_stage: "ready",
      initialization_progress_pct: 100,
      transport_mode: "direct",
      dcs: [],
      upstreams: [],
      recent_events: [],
    },
    last_seen_at: "2026-03-23T10:00:00Z",
  } as any);

  assert.equal(summary.locationText, "—");
  assert.equal(summary.metrics[0]?.label, "Connections");
  assert.equal(summary.metrics[0]?.value, "12");
  assert.equal(summary.metrics[1]?.value, "—");
  assert.equal(summary.metrics[2]?.value, "—");
  assert.equal(summary.dcTags.length, 0);
});

test("buildServerCardDetails preserves dc load precision", () => {
  const details = buildServerCardDetails({
    id: "server-2",
    node_name: "node-2",
    presence_state: "online",
    fleet_group_id: "group-b",
    version: "1.2.3",
    read_only: false,
    runtime: {
      me_runtime_ready: true,
      accepting_new_connections: true,
      degraded: false,
      current_connections: 5,
      current_connections_me: 3,
      current_connections_direct: 2,
      active_users: 4,
      connections_total: 0,
      connections_bad_total: 0,
      handshake_timeouts_total: 0,
      configured_users: 0,
      dc_coverage_pct: 100,
      healthy_upstreams: 2,
      total_upstreams: 2,
      use_middle_proxy: false,
      me2dc_fallback_enabled: false,
      startup_status: "ready",
      startup_stage: "ready",
      startup_progress_pct: 100,
      initialization_status: "ready",
      initialization_stage: "ready",
      initialization_progress_pct: 100,
      transport_mode: "direct",
      dcs: [
        {
          dc: 4,
          available_endpoints: 1,
          available_pct: 100,
          required_writers: 3,
          alive_writers: 3,
          coverage_pct: 100,
          rtt_ms: 18,
          load: 1.5,
        },
      ],
      upstreams: [],
      recent_events: [],
    },
    last_seen_at: "2026-03-23T10:00:00Z",
  } as any);

  assert.equal(details.isOffline, false);
  assert.equal(details.rows[0]?.loadText, "1.5");
});

test("buildFleetKpiSummary derives fleet totals from control room data", () => {
  const summary = buildFleetKpiSummary(
    {
      fleet: {
        total_agents: 3,
        online_agents: 2,
        degraded_agents: 1,
        offline_agents: 0,
        total_instances: 0,
        metric_snapshots: 0,
        live_connections: 19,
        accepting_new_connections_agents: 0,
        middle_proxy_agents: 0,
        dc_issue_agents: 0,
      },
      onboarding: {
        needs_first_server: false,
        setup_complete: true,
        suggested_fleet_group_id: "",
      },
      jobs: { total: 0, queued: 0, running: 0, failed: 0 },
      recent_activity: [],
      recent_runtime_events: [],
    } as any,
    [
      {
        runtime: {
          dc_coverage_pct: 100,
        },
      } as any,
      {
        runtime: {
          dc_coverage_pct: 50,
        },
      } as any,
    ],
    [
      { traffic_used_bytes: 400, active_tcp_conns: 10 } as any,
      { traffic_used_bytes: 600, active_tcp_conns: 9 } as any,
    ]
  );

  assert.equal(summary.totalServers, 3);
  assert.equal(summary.totalClients, 2);
  assert.equal(summary.activeConnections, 19);
  assert.equal(summary.totalTrafficBytes, 1000);
  assert.equal(summary.dcCoveragePct, 75);
});
