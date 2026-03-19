import test from "node:test";
import assert from "node:assert/strict";

import {
  buildClientDeploymentAlert,
  shouldPollClientDetail
} from "../src/lib/client-deployment-state.ts";
import type { ClientDeployment } from "../src/lib/api.ts";

test("shouldPollClientDetail keeps polling while deployments are queued", () => {
  assert.equal(shouldPollClientDetail([createDeployment({ status: "queued" })]), true);
});

test("shouldPollClientDetail stops polling once deployments are terminal", () => {
  assert.equal(shouldPollClientDetail([createDeployment({ status: "succeeded" })]), false);
  assert.equal(shouldPollClientDetail([createDeployment({ status: "failed" })]), false);
});

test("buildClientDeploymentAlert surfaces failed rollout details", () => {
  assert.deepEqual(
    buildClientDeploymentAlert([
      createDeployment({
        status: "failed",
        agent_id: "agent-1",
        last_error: "bad_request: secret must contain exactly 32 hexadecimal characters"
      })
    ]),
    {
      tone: "danger",
      title: "Client rollout failed",
      description: "agent-1: bad_request: secret must contain exactly 32 hexadecimal characters"
    }
  );
});

test("buildClientDeploymentAlert surfaces in-progress rollout state", () => {
  assert.deepEqual(
    buildClientDeploymentAlert([createDeployment({ status: "queued" })]),
    {
      tone: "warning",
      title: "Client rollout in progress",
      description: "Panvex is waiting for assigned nodes to apply the latest client state."
    }
  );
});

function createDeployment(overrides: Partial<ClientDeployment> = {}): ClientDeployment {
  return {
    agent_id: "agent-1",
    desired_operation: "client.create",
    status: "succeeded",
    last_error: "",
    connection_link: "",
    last_applied_at_unix: 0,
    updated_at_unix: 0,
    ...overrides
  };
}
