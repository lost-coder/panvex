// @ts-nocheck
import assert from "node:assert/strict";
import test from "node:test";

import { clientListItemSchema, clientListSchema, clientSchema } from "./client.ts";

const minimalListItem = {
  id: "c1",
  name: "Alice",
  enabled: true,
  assigned_nodes_count: 2,
  expiration_rfc3339: "2030-01-01T00:00:00Z",
  traffic_used_bytes: 0,
  unique_ips_used: 0,
  active_tcp_conns: 0,
  data_quota_bytes: 0,
  last_deploy_status: "ok",
};

test("clientListItemSchema accepts a minimal row", () => {
  const parsed = clientListItemSchema.parse(minimalListItem);
  assert.equal(parsed.id, "c1");
});

test("clientListSchema accepts an array of rows", () => {
  const parsed = clientListSchema.parse([minimalListItem, minimalListItem]);
  assert.equal(parsed.length, 2);
});

test("clientListItemSchema rejects missing name — UI would render 'undefined'", () => {
  const rest = { ...minimalListItem } as Record<string, unknown>;
  delete rest.name;
  const result = clientListItemSchema.safeParse(rest);
  assert.equal(result.success, false);
});

test("clientSchema requires deployments array (detail view)", () => {
  const detail = {
    ...minimalListItem,
    secret: "deadbeef",
    user_ad_tag: "",
    max_tcp_conns: 10,
    max_unique_ips: 5,
    fleet_group_ids: [],
    agent_ids: [],
    deployments: [],
    created_at_unix: 0,
    updated_at_unix: 0,
    deleted_at_unix: 0,
  };
  const parsed = clientSchema.parse(detail);
  assert.equal(parsed.secret, "deadbeef");
  assert.equal(parsed.deployments.length, 0);
});

test("clientSchema rejects missing secret — detail view must include it", () => {
  const detail = {
    ...minimalListItem,
    user_ad_tag: "",
    max_tcp_conns: 10,
    max_unique_ips: 5,
    fleet_group_ids: [],
    agent_ids: [],
    deployments: [],
    created_at_unix: 0,
    updated_at_unix: 0,
    deleted_at_unix: 0,
  };
  const result = clientSchema.safeParse(detail);
  assert.equal(result.success, false);
});
