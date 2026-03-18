import test from "node:test";
import assert from "node:assert/strict";

import {
  buildAgentAttentionList,
  buildAgentConnectionSummary,
  buildAgentModeLabel,
  buildAgentRuntimeStatus
} from "../src/lib/telemt-runtime-state.ts";
import type { Agent } from "../src/lib/api.ts";

test("buildAgentRuntimeStatus marks offline agents first", () => {
  const agent = createAgent({
    presence_state: "offline",
    runtime: {
      degraded: false,
      accepting_new_connections: true,
      startup_status: "ready",
      initialization_status: "ready",
      dc_coverage_pct: 100,
      healthy_upstreams: 2,
      total_upstreams: 2
    }
  });

  assert.equal(buildAgentRuntimeStatus(agent).label, "Offline");
});

test("buildAgentRuntimeStatus marks starting nodes before degraded checks", () => {
  const agent = createAgent({
    presence_state: "online",
    runtime: {
      degraded: true,
      accepting_new_connections: false,
      startup_status: "initializing",
      initialization_status: "initializing"
    }
  });

  assert.equal(buildAgentRuntimeStatus(agent).label, "Starting");
});

test("buildAgentRuntimeStatus marks degraded runtime signals", () => {
  const agent = createAgent({
    presence_state: "online",
    runtime: {
      degraded: false,
      accepting_new_connections: false,
      startup_status: "ready",
      initialization_status: "ready",
      dc_coverage_pct: 72,
      healthy_upstreams: 1,
      total_upstreams: 3
    }
  });

  assert.equal(buildAgentRuntimeStatus(agent).label, "Degraded");
});

test("buildAgentModeLabel resolves fallback before middle mode", () => {
  const agent = createAgent({
    runtime: {
      use_middle_proxy: true,
      me2dc_fallback_enabled: true,
      me_runtime_ready: false
    }
  });

  assert.equal(buildAgentModeLabel(agent), "Fallback");
});

test("buildAgentConnectionSummary shows ME and direct split", () => {
  const agent = createAgent({
    runtime: {
      current_connections: 42,
      current_connections_me: 39,
      current_connections_direct: 3
    }
  });

  assert.deepEqual(buildAgentConnectionSummary(agent), {
    primary: "42",
    secondary: "ME 39 / Direct 3"
  });
});

test("buildAgentAttentionList keeps the most urgent nodes first", () => {
  const offlineAgent = createAgent({
    id: "agent-offline",
    node_name: "node-offline",
    presence_state: "offline"
  });
  const degradedAgent = createAgent({
    id: "agent-degraded",
    node_name: "node-degraded",
    presence_state: "online",
    runtime: {
      degraded: true,
      startup_status: "ready",
      initialization_status: "ready"
    }
  });
  const healthyAgent = createAgent({
    id: "agent-healthy",
    node_name: "node-healthy",
    presence_state: "online"
  });

  assert.deepEqual(
    buildAgentAttentionList([healthyAgent, degradedAgent, offlineAgent]).map((agent) => agent.id),
    ["agent-offline", "agent-degraded"]
  );
});

function createAgent(overrides: Partial<Agent> = {}): Agent {
  return {
    id: "agent-1",
    node_name: "node-a",
    fleet_group_id: "default",
    version: "1.0.0",
    read_only: false,
    presence_state: "online",
    last_seen_at: "2026-03-18T10:00:00Z",
    runtime: {
      accepting_new_connections: true,
      me_runtime_ready: true,
      me2dc_fallback_enabled: false,
      use_middle_proxy: true,
      startup_status: "ready",
      startup_stage: "serving",
      startup_progress_pct: 100,
      initialization_status: "ready",
      degraded: false,
      initialization_stage: "serving",
      initialization_progress_pct: 100,
      transport_mode: "middle_proxy",
      current_connections: 10,
      current_connections_me: 8,
      current_connections_direct: 2,
      active_users: 4,
      connections_total: 100,
      connections_bad_total: 1,
      handshake_timeouts_total: 0,
      configured_users: 5,
      dc_coverage_pct: 100,
      healthy_upstreams: 2,
      total_upstreams: 2,
      dcs: [],
      upstreams: [],
      recent_events: []
    },
    ...overrides
  };
}
