// @ts-nocheck
import assert from "node:assert/strict";
import test from "node:test";

import { controlRoomSchema, dashboardSchema, fleetSchema } from "./dashboard.ts";

const minimalFleet = {
  total_agents: 1,
  online_agents: 1,
  degraded_agents: 0,
  offline_agents: 0,
  total_instances: 1,
  metric_snapshots: 0,
  live_connections: 0,
  accepting_new_connections_agents: 1,
  middle_proxy_agents: 0,
  dc_issue_agents: 0,
};

test("fleetSchema accepts a well-formed summary", () => {
  const parsed = fleetSchema.parse(minimalFleet);
  assert.equal(parsed.total_agents, 1);
});

test("fleetSchema rejects missing counter — prevents NaN in totals UI", () => {
  const rest = { ...minimalFleet } as Record<string, unknown>;
  delete rest.total_agents;
  const result = fleetSchema.safeParse(rest);
  assert.equal(result.success, false);
});

test("controlRoomSchema accepts an empty recent_activity", () => {
  const parsed = controlRoomSchema.parse({
    onboarding: {
      needs_first_server: false,
      setup_complete: true,
      suggested_fleet_group_id: "fg-default",
    },
    fleet: minimalFleet,
    jobs: { total: 0, queued: 0, running: 0, failed: 0 },
    recent_activity: [],
    recent_runtime_events: [],
  });
  assert.equal(parsed.jobs.total, 0);
});

test("dashboardSchema accepts an empty telemetry payload", () => {
  const parsed = dashboardSchema.parse({
    fleet: minimalFleet,
    attention: [],
    server_cards: [],
    runtime_distribution: {},
    recent_runtime_events: [],
  });
  assert.equal(parsed.attention.length, 0);
});
