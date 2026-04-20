// @ts-nocheck
import assert from "node:assert/strict";
import test from "node:test";

import { discoveredClientListSchema, discoveredClientSchema } from "./discovered.ts";

const minimal = {
  id: "d1",
  agent_id: "a1",
  node_name: "node-eu-1",
  client_name: "alice",
  status: "pending_review",
  total_octets: 0,
  current_connections: 0,
  active_unique_ips: 0,
  connection_link: "tg://proxy?server=...&port=443&secret=...",
  max_tcp_conns: 10,
  max_unique_ips: 5,
  data_quota_bytes: 0,
  expiration: "2030-01-01T00:00:00Z",
  discovered_at_unix: 1700000000,
  updated_at_unix: 1700000000,
};

test("discoveredClientSchema accepts a minimal pending_review row", () => {
  const parsed = discoveredClientSchema.parse(minimal);
  assert.equal(parsed.status, "pending_review");
});

test("discoveredClientSchema accepts optional conflicts array", () => {
  const parsed = discoveredClientSchema.parse({
    ...minimal,
    conflicts: [{ type: "same_secret_different_names", related_ids: ["x"] }],
  });
  assert.equal(parsed.conflicts?.[0].type, "same_secret_different_names");
});

test("discoveredClientSchema rejects unknown status", () => {
  const result = discoveredClientSchema.safeParse({ ...minimal, status: "quarantined" });
  assert.equal(result.success, false);
});

test("discoveredClientListSchema accepts empty list", () => {
  const parsed = discoveredClientListSchema.parse([]);
  assert.equal(parsed.length, 0);
});
