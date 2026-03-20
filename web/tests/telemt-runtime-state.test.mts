import test from "node:test";
import assert from "node:assert/strict";

import {
  buildAgentAttentionList,
  buildAgentAttentionReasons,
  buildAgentConnectionSummary,
  buildAgentDCIssueSummary,
  buildAgentDCSegments,
  buildAgentDCCoverageStage,
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

test("buildAgentAttentionReasons explains why a node needs attention", () => {
  const agent = createAgent({
    runtime: {
      degraded: true,
      accepting_new_connections: false,
      dc_coverage_pct: 72,
      healthy_upstreams: 1,
      total_upstreams: 3
    }
  });

  assert.deepEqual(buildAgentAttentionReasons(agent).map((reason) => reason.label), [
    "Runtime degraded",
    "Admissions paused",
    "Reduced DC coverage",
    "Upstreams degraded"
  ]);
});

test("buildAgentAttentionReasons collapses offline nodes into one clear reason", () => {
  const agent = createAgent({
    presence_state: "offline",
    runtime: {
      degraded: true,
      accepting_new_connections: false,
      dc_coverage_pct: 0
    }
  });

  assert.deepEqual(buildAgentAttentionReasons(agent).map((reason) => reason.label), ["Offline"]);
});

test("buildAgentDCCoverageStage maps coverage into four discrete stages", () => {
  assert.equal(
    buildAgentDCCoverageStage(
      createAgent({
        runtime: { dc_coverage_pct: 100 }
      })
    ),
    100
  );
  assert.equal(
    buildAgentDCCoverageStage(
      createAgent({
        runtime: { dc_coverage_pct: 84 }
      })
    ),
    66
  );
  assert.equal(
    buildAgentDCCoverageStage(
      createAgent({
        runtime: { dc_coverage_pct: 15 }
      })
    ),
    33
  );
  assert.equal(
    buildAgentDCCoverageStage(
      createAgent({
        runtime: { dc_coverage_pct: 0 }
      })
    ),
    0
  );
});

test("buildAgentDCSegments maps each DC into a radial stage", () => {
  const agent = createAgent({
    runtime: {
      dcs: [
        {
          dc: 4,
          available_endpoints: 6,
          available_pct: 100,
          required_writers: 2,
          alive_writers: 2,
          coverage_pct: 100,
          rtt_ms: 42,
          load: 0.2
        },
        {
          dc: 2,
          available_endpoints: 5,
          available_pct: 100,
          required_writers: 2,
          alive_writers: 2,
          coverage_pct: 74,
          rtt_ms: 38,
          load: 0.25
        },
        {
          dc: 5,
          available_endpoints: 3,
          available_pct: 50,
          required_writers: 2,
          alive_writers: 1,
          coverage_pct: 22,
          rtt_ms: 91,
          load: 0.51
        },
        {
          dc: 1,
          available_endpoints: 0,
          available_pct: 0,
          required_writers: 2,
          alive_writers: 0,
          coverage_pct: 0,
          rtt_ms: 0,
          load: 0
        }
      ]
    }
  });

  assert.deepEqual(
    buildAgentDCSegments(agent).map((segment) => ({
      dc: segment.dc,
      stage: segment.stage,
      label: segment.label,
      stateLabel: segment.stateLabel
    })),
    [
      { dc: 1, stage: 0, label: "DC1", stateLabel: "Down" },
      { dc: 2, stage: 66, label: "DC2", stateLabel: "Reduced" },
      { dc: 4, stage: 100, label: "DC4", stateLabel: "Full" },
      { dc: 5, stage: 33, label: "DC5", stateLabel: "Limited" }
    ]
  );
});

test("buildAgentDCIssueSummary counts OK DCs and sorts issues by severity", () => {
  const agent = createAgent({
    runtime: {
      dcs: [
        {
          dc: 4,
          available_endpoints: 6,
          available_pct: 100,
          required_writers: 2,
          alive_writers: 2,
          coverage_pct: 100,
          rtt_ms: 42,
          load: 0.2
        },
        {
          dc: 2,
          available_endpoints: 5,
          available_pct: 100,
          required_writers: 2,
          alive_writers: 2,
          coverage_pct: 74,
          rtt_ms: 38,
          load: 0.25
        },
        {
          dc: 5,
          available_endpoints: 3,
          available_pct: 50,
          required_writers: 2,
          alive_writers: 1,
          coverage_pct: 22,
          rtt_ms: 91,
          load: 0.51
        },
        {
          dc: 1,
          available_endpoints: 0,
          available_pct: 0,
          required_writers: 2,
          alive_writers: 0,
          coverage_pct: 0,
          rtt_ms: 0,
          load: 0
        }
      ]
    }
  });

  assert.deepEqual(buildAgentDCIssueSummary(agent), {
    totalCount: 4,
    okCount: 1,
    issueCount: 3,
    issues: [
      { dc: 1, label: "DC1 down", stateLabel: "Down", tone: "rose" },
      { dc: 5, label: "DC5 limited", stateLabel: "Limited", tone: "amber" },
      { dc: 2, label: "DC2 reduced", stateLabel: "Reduced", tone: "sky" }
    ]
  });
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
