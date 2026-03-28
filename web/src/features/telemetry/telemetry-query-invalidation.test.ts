// @ts-nocheck
import assert from "node:assert/strict";
import test from "node:test";

import { invalidateTelemetryQueries } from "./telemetry-query-invalidation.ts";

test("invalidateTelemetryQueries refreshes dashboard, servers, and all detail queries by default", async () => {
  const calls = [];
  const queryClient = {
    invalidateQueries: async (input) => {
      calls.push(input);
    },
  };

  await invalidateTelemetryQueries(queryClient);

  assert.equal(calls.length, 3);
  assert.deepEqual(calls[0], { queryKey: ["telemetry-dashboard"] });
  assert.deepEqual(calls[1], { queryKey: ["telemetry-servers"] });
  assert.equal(typeof calls[2]?.predicate, "function");
});

test("invalidateTelemetryQueries refreshes a targeted detail query when agent id is provided", async () => {
  const calls = [];
  const queryClient = {
    invalidateQueries: async (input) => {
      calls.push(input);
    },
  };

  await invalidateTelemetryQueries(queryClient, "agent-a");

  assert.equal(calls.length, 3);
  assert.deepEqual(calls[2], { queryKey: ["telemetry-server", "agent-a"] });
});
