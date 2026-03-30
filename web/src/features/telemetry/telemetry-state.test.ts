// @ts-nocheck
import assert from "node:assert/strict";
import test from "node:test";
import { telemetryServerDetailRefetchInterval } from "./telemetry-refetch.ts";

test("telemetryServerDetailRefetchInterval uses fast refresh while initialization watch is visible", () => {
  const interval = telemetryServerDetailRefetchInterval({
    initialization_watch: {
      visible: true,
    },
  });

  assert.equal(interval, 3_000);
});

test("telemetryServerDetailRefetchInterval falls back to standard refresh when initialization watch is hidden", () => {
  const interval = telemetryServerDetailRefetchInterval({
    initialization_watch: {
      visible: false,
    },
  });

  assert.equal(interval, 15_000);
});
