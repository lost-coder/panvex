import { describe, expect, it } from "vitest";

import type {
  AgentRuntime,
  TelemetryServerDetailResponse,
} from "@/shared/api/api";

import { transformServerDetail } from "./servers";

// Minimal AgentRuntime fixture. Tests override individual fields rather
// than rebuilding the whole object — keeps each case focused on the
// piece of behaviour it actually exercises.
function makeRuntime(overrides: Partial<AgentRuntime> = {}): AgentRuntime {
  return {
    accepting_new_connections: true,
    me_runtime_ready: true,
    me2dc_fallback_enabled: false,
    use_middle_proxy: false,
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
    healthy_upstreams: 2,
    total_upstreams: 3,
    dcs: [],
    upstreams: [
      {
        upstream_id: 1,
        route_kind: "direct",
        address: "1.1.1.1:443",
        healthy: true,
        fails: 0,
        effective_latency_ms: 10,
        weight: 1,
        last_check_age_secs: 5,
      },
      {
        upstream_id: 2,
        route_kind: "direct",
        address: "2.2.2.2:443",
        healthy: true,
        fails: 0,
        effective_latency_ms: 12,
        weight: 1,
        last_check_age_secs: 5,
      },
      {
        upstream_id: 3,
        route_kind: "socks5",
        address: "3.3.3.3:1080",
        healthy: false,
        fails: 4,
        effective_latency_ms: 0,
        weight: 1,
        last_check_age_secs: 30,
      },
    ],
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
    ...overrides,
  };
}

function makeDetailResponse(runtime: AgentRuntime): TelemetryServerDetailResponse {
  return {
    server: {
      agent: {
        id: "a-1",
        node_name: "node-1",
        fleet_group_id: "fg-1",
        version: "1.0.0",
        read_only: false,
        presence_state: "online",
        runtime,
        last_seen_at: "2024-01-01T00:00:00Z",
      },
      severity: "ok",
      reason: "",
      runtime_freshness: { state: "fresh", observed_at_unix: 0 },
      detail_boost: { active: false, expires_at_unix: 0, remaining_seconds: 0 },
    },
    initialization_watch: {
      visible: false,
      mode: "hidden",
      remaining_seconds: 0,
      completed_at_unix: 0,
      startup_status: "ready",
      startup_stage: "done",
      startup_progress_pct: 100,
      initialization_status: "ready",
      initialization_stage: "done",
      initialization_progress_pct: 100,
    },
    diagnostics: {
      state: "fresh",
      state_reason: "",
      system_info: {},
      effective_limits: {},
      security_posture: {},
      minimal_all: {},
      me_pool: {},
      dcs_detail: {},
    },
    security_inventory: {
      state: "fresh",
      state_reason: "",
      enabled: false,
      entries_total: 0,
      entries: [],
    },
  };
}

describe("transformServerDetail", () => {
  it("populates upstreamSummary from runtime-root fail-rate fields", () => {
    const runtime = makeRuntime({
      fail_rate_pct_5m: 7.5,
      fail_rate_known: true,
      connect_attempt_total: 200,
      connect_success_total: 185,
      connect_fail_total: 15,
      connect_failfast_total: 4,
    });
    const result = transformServerDetail(makeDetailResponse(runtime));

    expect(result.upstreamSummary).toBeDefined();
    const summary = result.upstreamSummary!;
    expect(summary.failRatePct5m).toBe(7.5);
    expect(summary.failRateKnown).toBe(true);
    expect(summary.connectAttemptTotal).toBe(200);
    expect(summary.connectSuccessTotal).toBe(185);
    expect(summary.connectFailTotal).toBe(15);
    expect(summary.connectFailfastTotal).toBe(4);
    // healthyTotal / configuredTotal flow from runtime aggregates.
    expect(summary.healthyTotal).toBe(2);
    expect(summary.configuredTotal).toBe(3);
    expect(summary.unhealthyTotal).toBe(1);
    // Route-kind buckets are derived from the upstreams list.
    expect(summary.directTotal).toBe(2);
    expect(summary.socks5Total).toBe(1);
    expect(summary.socks4Total).toBe(0);
    expect(summary.shadowsocksTotal).toBe(0);
  });

  it("defaults upstreamSummary fields to 0/false when an old agent omits them", () => {
    // Old agents (pre-Phase-3) don't emit fail_rate_* — zod default is 0,
    // failRateKnown defaults to false. The transform must surface that
    // honestly so the UI shows "unknown" rather than fabricating 0%.
    const runtime = makeRuntime();
    const result = transformServerDetail(makeDetailResponse(runtime));

    const summary = result.upstreamSummary!;
    expect(summary.failRatePct5m).toBe(0);
    expect(summary.failRateKnown).toBe(false);
    expect(summary.connectAttemptTotal).toBe(0);
  });

  it("flows fallback_entered_at_unix through to fallbackEnteredAtUnix", () => {
    const runtime = makeRuntime({
      me2dc_fallback_enabled: true,
      fallback_entered_at_unix: 1_700_000_000,
    });
    const result = transformServerDetail(makeDetailResponse(runtime));
    expect(result.fallbackEnteredAtUnix).toBe(1_700_000_000);
  });

  it("falls back to null when fallback_entered_at_unix is absent", () => {
    const runtime = makeRuntime();
    const result = transformServerDetail(makeDetailResponse(runtime));
    expect(result.fallbackEnteredAtUnix).toBeNull();
  });
});
