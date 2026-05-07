/**
 * Playwright fixtures for /api/telemetry/servers/{id} responses.
 *
 * The smoke specs intercept the server-detail endpoint via
 * `page.route()` and fulfill with one of these helpers, so each test
 * stays hermetic — no backend, no database. The shape mirrors
 * `TelemetryServerDetailResponse` in src/shared/api/telemetry.ts.
 *
 * Direct-mode (`use_middle_proxy=false`, `me_runtime_ready=false`) is
 * the canonical Telemt deployment for self-hosted relays — no ME pool,
 * upstreams resolved by the agent itself. The fixture has three healthy
 * upstreams so the direct-relay layout has data to render and the DC
 * tiles stay hidden (no `dcs` entries).
 */

export interface MockDirectAgentOverrides {
  agentId?: string;
  nodeName?: string;
  fleetGroupId?: string;
  healthyUpstreams?: number;
  totalUpstreams?: number;
}

/**
 * mockTelemtUnreachableAgent returns the same shape as mockDirectAgent but
 * with telemt_reachable=false so e2e specs can exercise the
 * banner-and-hidden-mode rendering path.
 */
export function mockTelemtUnreachableAgent(
  overrides: { agentId?: string; nodeName?: string; sinceUnixOffsetSec?: number } = {},
) {
  const base = mockDirectAgent({
    agentId: overrides.agentId ?? "agent-telemt-down-1",
    nodeName: overrides.nodeName ?? "node-telemt-down",
  });
  const offsetSec = overrides.sinceUnixOffsetSec ?? 90;
  // Clone deeply so we don't mutate the shared base object on subsequent calls.
  const out = structuredClone(base);
  out.server.agent.runtime.telemt_reachable = false;
  out.server.agent.runtime.telemt_unreachable_since_unix =
    Math.floor(Date.now() / 1000) - offsetSec;
  // Zero the runtime metrics — they should not appear in the UI.
  out.server.agent.runtime.current_connections = 0;
  out.server.agent.runtime.current_connections_me = 0;
  out.server.agent.runtime.current_connections_direct = 0;
  out.server.agent.runtime.dc_coverage_pct = 0;
  return out;
}

export function mockDirectAgent(overrides: MockDirectAgentOverrides = {}) {
  const agentId = overrides.agentId ?? "agent-direct-1";
  const nodeName = overrides.nodeName ?? "node-direct";
  const fleetGroupId = overrides.fleetGroupId ?? "default";
  const healthy = overrides.healthyUpstreams ?? 3;
  const total = overrides.totalUpstreams ?? 3;

  return {
    server: {
      agent: {
        id: agentId,
        node_name: nodeName,
        fleet_group_id: fleetGroupId,
        version: "1.2.3",
        read_only: false,
        presence_state: "online",
        cert_issued_at: "2026-04-01T00:00:00Z",
        cert_expires_at: "2026-05-01T00:00:00Z",
        last_seen_at: "2026-04-29T12:00:00Z",
        runtime: {
          accepting_new_connections: true,
          me_runtime_ready: false,
          me2dc_fallback_enabled: false,
          use_middle_proxy: false,
          startup_status: "ready",
          startup_stage: "ready",
          startup_progress_pct: 100,
          initialization_status: "ready",
          initialization_stage: "ready",
          initialization_progress_pct: 100,
          degraded: false,
          transport_mode: "direct",
          current_connections: 5,
          current_connections_me: 0,
          current_connections_direct: 5,
          active_users: 1,
          uptime_seconds: 1234,
          connections_total: 100,
          connections_bad_total: 0,
          handshake_timeouts_total: 0,
          configured_users: 10,
          dc_coverage_pct: 0,
          healthy_upstreams: healthy,
          total_upstreams: total,
          reroute_active: false,
          stale_cache_used: false,
          top_by_connections: [],
          top_by_throughput: [],
          dcs: [],
          upstreams: Array.from({ length: total }, (_, i) => ({
            upstream_id: i + 1,
            route_kind: "direct",
            address: `10.0.0.${i + 1}:443`,
            healthy: i < healthy,
            fails: 0,
            effective_latency_ms: 12.5,
            weight: 1,
            last_check_age_secs: 1,
            scopes: [],
            fail_rate_pct_5m: 0,
            fail_rate_known: true,
            connect_attempt_total: 0,
            connect_success_total: 0,
            connect_fail_total: 0,
            connect_failfast_total: 0,
          })),
          recent_events: [],
          system_load: {
            cpu_usage_pct: 5,
            memory_used_bytes: 1_000_000,
            memory_total_bytes: 8_000_000,
            memory_usage_pct: 12.5,
            disk_used_bytes: 1_000_000,
            disk_total_bytes: 100_000_000,
            disk_usage_pct: 1,
            load_1m: 0.1,
            load_5m: 0.1,
            load_15m: 0.1,
            net_bytes_sent: 0,
            net_bytes_recv: 0,
          },
        },
      },
      severity: "good",
      reason: "",
      runtime_freshness: {
        state: "fresh",
        observed_at_unix: Math.floor(Date.now() / 1000),
      },
      detail_boost: {
        active: false,
        expires_at_unix: 0,
        remaining_seconds: 0,
      },
    },
    initialization_watch: {
      visible: false,
      mode: "hidden",
      remaining_seconds: 0,
      completed_at_unix: 0,
      startup_status: "ready",
      startup_stage: "ready",
      startup_progress_pct: 100,
      initialization_status: "ready",
      initialization_stage: "ready",
      initialization_progress_pct: 100,
    },
    diagnostics: {
      state: "ready",
      state_reason: "",
      system_info: {
        version: "1.2.3",
        target_arch: "x86_64",
        target_os: "linux",
        build_profile: "release",
        config_hash: "abc",
        config_path: "/etc/telemt.toml",
        config_reload_count: 0,
      },
      effective_limits: {},
      security_posture: {},
      minimal_all: {},
      me_pool: { enabled: false },
      dcs_detail: { dcs: [] },
    },
    security_inventory: {
      state: "ready",
      state_reason: "",
      enabled: false,
      entries_total: 0,
      entries: [],
    },
  };
}
