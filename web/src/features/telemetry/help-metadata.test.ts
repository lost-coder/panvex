// @ts-nocheck
import assert from "node:assert/strict";
import test from "node:test";

import { getTelemetryFieldHelp } from "./help-metadata.ts";

test("getTelemetryFieldHelp returns copy for known telemetry labels", () => {
  assert.match(getTelemetryFieldHelp("Config Hash") ?? "", /config fingerprint/i);
  assert.match(getTelemetryFieldHelp("Active Generation") ?? "", /ME pool generation/i);
  assert.equal(getTelemetryFieldHelp("Unknown Field"), undefined);
});
