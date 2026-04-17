// @ts-nocheck
import assert from "node:assert/strict";
import test from "node:test";

import { apiErrorSchema, id, timestamp, unixSeconds } from "./common.ts";

test("id rejects empty string", () => {
  const result = id.safeParse("");
  assert.equal(result.success, false);
});

test("id accepts arbitrary non-empty string", () => {
  assert.equal(id.parse("anything"), "anything");
});

test("timestamp accepts a plain string (loose by design)", () => {
  assert.equal(timestamp.parse("2024-01-01T00:00:00Z"), "2024-01-01T00:00:00Z");
});

test("unixSeconds rejects floats", () => {
  const result = unixSeconds.safeParse(1.5);
  assert.equal(result.success, false);
});

test("apiErrorSchema accepts empty object (all fields optional)", () => {
  const parsed = apiErrorSchema.parse({});
  assert.equal(parsed.error, undefined);
});

test("apiErrorSchema accepts error + code", () => {
  const parsed = apiErrorSchema.parse({ error: "nope", code: "E_NOPE" });
  assert.equal(parsed.code, "E_NOPE");
});
