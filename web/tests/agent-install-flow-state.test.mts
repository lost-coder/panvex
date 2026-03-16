import test from "node:test";
import assert from "node:assert/strict";

import { resolveConnectedAgentID } from "../src/components/agent-install-flow-state.ts";
import { type Agent, type Instance } from "../src/lib/api.ts";

test("resolveConnectedAgentID waits for the first runtime instance", () => {
  const agents: Agent[] = [
    {
      id: "agent-existing",
      node_name: "existing-node",
      environment_id: "prod",
      fleet_group_id: "default",
      version: "1.0.0",
      read_only: false,
      last_seen_at: "2026-03-16T12:00:00Z"
    },
    {
      id: "agent-new",
      node_name: "new-node",
      environment_id: "prod",
      fleet_group_id: "default",
      version: "1.0.0",
      read_only: false,
      last_seen_at: "2026-03-16T12:01:00Z"
    }
  ];
  const instances: Instance[] = [];

  assert.equal(resolveConnectedAgentID(agents, instances, ["agent-existing"]), null);
});

test("resolveConnectedAgentID accepts a new agent after the first runtime instance appears", () => {
  const agents: Agent[] = [
    {
      id: "agent-existing",
      node_name: "existing-node",
      environment_id: "prod",
      fleet_group_id: "default",
      version: "1.0.0",
      read_only: false,
      last_seen_at: "2026-03-16T12:00:00Z"
    },
    {
      id: "agent-new",
      node_name: "new-node",
      environment_id: "prod",
      fleet_group_id: "default",
      version: "1.0.0",
      read_only: false,
      last_seen_at: "2026-03-16T12:01:00Z"
    }
  ];
  const instances: Instance[] = [
    {
      id: "telemt-primary",
      agent_id: "agent-new",
      name: "telemt-primary",
      version: "2026.03",
      config_fingerprint: "runtime",
      connected_users: 0,
      read_only: false,
      updated_at: "2026-03-16T12:01:00Z"
    }
  ];

  assert.equal(resolveConnectedAgentID(agents, instances, ["agent-existing"]), "agent-new");
});
