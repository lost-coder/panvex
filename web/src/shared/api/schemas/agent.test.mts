import assert from "node:assert/strict";
import test from "node:test";

import { agentListSchema, agentSchema } from "./agent.ts";

const minimalRuntime = {
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
  transport_mode: "classic",
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

const minimalAgent = {
  id: "a1",
  node_name: "node-eu-1",
  fleet_group_id: "fg-default",
  version: "1.0.0",
  read_only: false,
  presence_state: "online",
  runtime: minimalRuntime,
  last_seen_at: "2024-01-01T00:00:00Z",
};

test("agentSchema accepts a well-formed online agent", () => {
  const parsed = agentSchema.parse(minimalAgent);
  assert.equal(parsed.id, "a1");
  assert.equal(parsed.runtime.transport_mode, "classic");
});

test("agentListSchema accepts an empty fleet", () => {
  const parsed = agentListSchema.parse([]);
  assert.equal(parsed.length, 0);
});

test("agentSchema rejects missing runtime — catches DF-10 drift", () => {
  const rest = { ...minimalAgent } as Record<string, unknown>;
  delete rest.runtime;
  const result = agentSchema.safeParse(rest);
  assert.equal(result.success, false);
});

test("agentSchema rejects wrong type for node_name", () => {
  const result = agentSchema.safeParse({ ...minimalAgent, node_name: 42 });
  assert.equal(result.success, false);
});

test("agentRuntimeSchema accepts the direct-mode fail-rate fields at the runtime root", () => {
  const runtime = {
    ...minimalRuntime,
    fail_rate_pct_5m: 12.5,
    fail_rate_known: true,
    connect_attempt_total: 100,
    connect_success_total: 90,
    connect_fail_total: 10,
    connect_failfast_total: 2,
    fallback_entered_at_unix: 1_700_000_000,
  };
  const parsed = agentSchema.parse({ ...minimalAgent, runtime });
  assert.equal(parsed.runtime.fail_rate_pct_5m, 12.5);
  assert.equal(parsed.runtime.fail_rate_known, true);
  assert.equal(parsed.runtime.connect_attempt_total, 100);
  assert.equal(parsed.runtime.connect_success_total, 90);
  assert.equal(parsed.runtime.connect_fail_total, 10);
  assert.equal(parsed.runtime.connect_failfast_total, 2);
  assert.equal(parsed.runtime.fallback_entered_at_unix, 1_700_000_000);
});

test("agentRuntimeSchema defaults the direct-mode fail-rate fields when absent (old agents)", () => {
  // Pre-Phase-3 agents don't emit these counters at the runtime root.
  // The schema must fill zeros + false rather than failing the parse.
  const parsed = agentSchema.parse(minimalAgent);
  assert.equal(parsed.runtime.fail_rate_pct_5m, 0);
  assert.equal(parsed.runtime.fail_rate_known, false);
  assert.equal(parsed.runtime.connect_attempt_total, 0);
  assert.equal(parsed.runtime.fallback_entered_at_unix, undefined);
});
