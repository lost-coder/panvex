import { describe, expect, it } from "vitest";

import type {
  Agent,
  AgentRuntime,
  TelemetryDashboardResponse,
} from "@/shared/api/api";

import { transformDashboardOverview } from "./dashboard";

// Backend severity vocabulary moved to "ok"/"warn"/"critical"/"bad" in
// Phase-3, but the dashboard transform was still filtering for the
// legacy "good" tag. The result was a permanently empty Healthy list:
// FleetPanel rendered "No servers registered yet." even on a fully
// online fleet. These tests pin the new behaviour so the regression
// doesn't recur.

const baseRuntime: AgentRuntime = {
  accepting_new_connections: true,
  me_runtime_ready: true,
  me2dc_fallback_enabled: false,
  use_middle_proxy: false,
  telemt_unreachable: false,
  telemt_unreachable_since_unix: 0,
  startup_status: "ready",
  startup_stage: "done",
  startup_progress_pct: 100,
  initialization_status: "ready",
  degraded: false,
  initialization_stage: "done",
  initialization_progress_pct: 100,
  transport_mode: "direct",
  current_connections: 0,
  current_connections_me: 0,
  current_connections_direct: 0,
  active_users: 0,
  uptime_seconds: 0,
  connections_total: 0,
  connections_bad_total: 0,
  handshake_timeouts_total: 0,
  configured_users: 0,
  dc_coverage_pct: 100,
  healthy_upstreams: 1,
  total_upstreams: 1,
  dcs: [],
  upstreams: [],
  recent_events: [],
  system_load: {
    cpu_usage_pct: 0,
    memory_used_bytes: 0,
    memory_total_bytes: 0,
    memory_usage_pct: 0,
    disk_used_bytes: 0,
    disk_total_bytes: 0,
    disk_usage_pct: 0,
    load_1m: 0,
    load_5m: 0,
    load_15m: 0,
    net_bytes_sent: 0,
    net_bytes_recv: 0,
  },
};

function makeAgent(id: string, name: string, runtime = baseRuntime): Agent {
  return {
    id,
    node_name: name,
    fleet_group_id: "fg-1",
    version: "1.0.0",
    read_only: false,
    presence_state: "online",
    runtime,
    last_seen_at: "2026-05-04T12:00:00Z",
  };
}

const minimalFleet: TelemetryDashboardResponse["fleet"] = {
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

function makeResponse(
  serverCards: TelemetryDashboardResponse["server_cards"],
  attention: TelemetryDashboardResponse["attention"] = [],
): TelemetryDashboardResponse {
  return {
    fleet: { ...minimalFleet, total_agents: serverCards.length, online_agents: serverCards.length },
    attention,
    server_cards: serverCards,
    runtime_distribution: {},
    recent_runtime_events: [],
    recent_events: [],
    agent_load_series: [],
  };
}

describe("transformDashboardOverview", () => {
  it("places severity='ok' fresh nodes into the Healthy bucket", () => {
    const overview = transformDashboardOverview(
      makeResponse([
        {
          agent: makeAgent("a-1", "node-a"),
          severity: "ok",
          reason: "",
          runtime_freshness: { state: "fresh", observed_at_unix: 0 },
          detail_boost: { active: false, expires_at_unix: 0, remaining_seconds: 0 },
          traffic_bytes: 0,
        },
      ]),
    );
    expect(overview.healthyNodes).toHaveLength(1);
    expect(overview.healthyNodes[0]!.id).toBe("a-1");
    expect(overview.attentionNodes).toHaveLength(0);
  });

  it("keeps a stale-but-healthy node in attention only (no double-render)", () => {
    const agent = makeAgent("a-2", "node-stale");
    const overview = transformDashboardOverview(
      makeResponse(
        [
          {
            agent,
            severity: "ok",
            reason: "",
            runtime_freshness: { state: "stale", observed_at_unix: 0 },
            detail_boost: { active: false, expires_at_unix: 0, remaining_seconds: 0 },
            traffic_bytes: 0,
          },
        ],
        [
          {
            agent_id: "a-2",
            node_name: "node-stale",
            fleet_group_id: "fg-1",
            severity: "ok",
            reason: "Telemetry is stale",
            presence_state: "online",
            runtime: agent.runtime,
            runtime_freshness: { state: "stale", observed_at_unix: 0 },
            detail_boost: { active: false, expires_at_unix: 0, remaining_seconds: 0 },
          },
        ],
      ),
    );
    expect(overview.attentionNodes).toHaveLength(1);
    expect(overview.attentionNodes[0]!.id).toBe("a-2");
    expect(overview.healthyNodes).toHaveLength(0);
  });

  it("excludes Direct-mode nodes from fleet-wide DC coverage average", () => {
    const meAgent = makeAgent("a-me", "node-me", {
      ...baseRuntime,
      use_middle_proxy: true,
      dc_coverage_pct: 100,
    });
    const directAgent = makeAgent("a-direct", "node-direct", {
      ...baseRuntime,
      use_middle_proxy: false,
      dc_coverage_pct: 0,
    });
    const overview = transformDashboardOverview(
      makeResponse([
        {
          agent: meAgent,
          severity: "ok",
          reason: "",
          runtime_freshness: { state: "fresh", observed_at_unix: 0 },
          detail_boost: { active: false, expires_at_unix: 0, remaining_seconds: 0 },
          traffic_bytes: 0,
        },
        {
          agent: directAgent,
          severity: "ok",
          reason: "",
          runtime_freshness: { state: "fresh", observed_at_unix: 0 },
          detail_boost: { active: false, expires_at_unix: 0, remaining_seconds: 0 },
          traffic_bytes: 0,
        },
      ]),
    );
    const coverageKpi = overview.kpis.find((k) => k.label === "DC coverage");
    expect(coverageKpi).toBeDefined();
    // Without the filter the average would be (100+0)/2 = 50% and the
    // tone would be "error". With the filter only the ME node counts.
    expect(coverageKpi?.value).toBe("100%");
    expect(coverageKpi?.tone).toBe("ok");
  });

  it("shows 'n/a' for DC coverage when the fleet is all Direct mode", () => {
    const directAgent = makeAgent("a-direct", "node-direct", {
      ...baseRuntime,
      use_middle_proxy: false,
      dc_coverage_pct: 0,
    });
    const overview = transformDashboardOverview(
      makeResponse([
        {
          agent: directAgent,
          severity: "ok",
          reason: "",
          runtime_freshness: { state: "fresh", observed_at_unix: 0 },
          detail_boost: { active: false, expires_at_unix: 0, remaining_seconds: 0 },
          traffic_bytes: 0,
        },
      ]),
    );
    const coverageKpi = overview.kpis.find((k) => k.label === "DC coverage");
    expect(coverageKpi?.value).toBe("n/a");
    expect(coverageKpi?.tone).toBe("default");
  });

  it("derives state + reason for attention nodes and defaults healthy nodes", () => {
    const raw = {
      fleet: { online_agents: 1, total_agents: 2, offline_agents: 1, degraded_agents: 0, live_connections: 0, dc_issue_agents: 0 },
      attention: [
        {
          agent_id: "a1",
          node_name: "edge-1",
          fleet_group_id: "g",
          severity: "bad",
          reason: "Agent heartbeat is offline",
          presence_state: "offline",
          runtime: { telemt_unreachable: false },
          runtime_freshness: { observed_at_unix: 0 },
          detail_boost: {},
        },
      ],
      server_cards: [
        {
          agent: { id: "a2", node_name: "edge-2", version: "1", runtime: { current_connections: 5, system_load: { cpu_usage_pct: 10, memory_usage_pct: 20 }, dcs: [] } },
          severity: "ok",
          reason: "",
          runtime_freshness: { observed_at_unix: 0 },
        },
      ],
    } as unknown as Parameters<typeof transformDashboardOverview>[0];
    const out = transformDashboardOverview(raw);
    expect(out.attentionNodes[0]!.state).toBe("offline");
    expect(out.attentionNodes[0]!.reason).toBe("Agent heartbeat is offline");
    expect(out.healthyNodes[0]!.state).toBe("ok");
    expect(out.healthyNodes[0]!.reason).toBe("");
  });

  it("does not surface healthy stale agents as alerts (severity is still ok)", () => {
    const agent = makeAgent("a-3", "node-stale-2");
    const overview = transformDashboardOverview(
      makeResponse(
        [],
        [
          {
            agent_id: "a-3",
            node_name: "node-stale-2",
            fleet_group_id: "fg-1",
            severity: "ok",
            reason: "Telemetry is stale",
            presence_state: "online",
            runtime: agent.runtime,
            runtime_freshness: { state: "stale", observed_at_unix: 0 },
            detail_boost: { active: false, expires_at_unix: 0, remaining_seconds: 0 },
          },
        ],
      ),
    );
    expect(overview.alerts).toHaveLength(0);
  });
});
