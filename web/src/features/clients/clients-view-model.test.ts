// @ts-nocheck
import test from "node:test";
import assert from "node:assert/strict";

import {
  buildClientFilterCounts,
  buildClientTableRows,
  filterClientTableRows,
  paginateClientTableRows,
  sortClientTableRows,
} from "./clients-view-model.ts";

const NOW = Date.UTC(2026, 2, 24, 12, 0, 0);

test("buildClientTableRows maps clients into display rows with status, quota, traffic, and expires texts", () => {
  const rows = buildClientTableRows([
    {
      id: "client-1",
      name: "Acme Alpha",
      enabled: true,
      assigned_nodes_count: 3,
      expiration_rfc3339: "2026-04-03T12:00:00Z",
      traffic_used_bytes: 1610612736,
      unique_ips_used: 2,
      active_tcp_conns: 12,
      data_quota_bytes: 5368709120,
      last_deploy_status: "success",
    } as any,
  ], NOW);

  assert.equal(rows[0]?.clientName, "Acme Alpha");
  assert.equal(rows[0]?.deployStatusText, "Success");
  assert.equal(rows[0]?.statusText, "Active");
  assert.equal(rows[0]?.statusTone, "active");
  assert.equal(rows[0]?.connectionsText, "12");
  assert.equal(rows[0]?.serversText, "3");
  assert.equal(rows[0]?.trafficText, "1.5 GB");
  assert.equal(rows[0]?.quotaText, "1.5 GB / 5.0 GB");
  assert.equal(rows[0]?.expiresPrimaryText, "in 10d");
  assert.equal(rows[0]?.expiresSecondaryText, "Apr 03, 2026");
});

test("buildClientFilterCounts and filterClientTableRows split active, idle, and disabled rows correctly", () => {
  const rows = buildClientTableRows([
    {
      id: "active",
      name: "Active client",
      enabled: true,
      assigned_nodes_count: 2,
      expiration_rfc3339: "2026-04-03T12:00:00Z",
      traffic_used_bytes: 100,
      unique_ips_used: 1,
      active_tcp_conns: 3,
      data_quota_bytes: 500,
      last_deploy_status: "success",
    } as any,
    {
      id: "idle",
      name: "Idle client",
      enabled: true,
      assigned_nodes_count: 1,
      expiration_rfc3339: "2026-04-03T12:00:00Z",
      traffic_used_bytes: 100,
      unique_ips_used: 1,
      active_tcp_conns: 0,
      data_quota_bytes: 500,
      last_deploy_status: "pending_update",
    } as any,
    {
      id: "disabled",
      name: "Disabled client",
      enabled: false,
      assigned_nodes_count: 0,
      expiration_rfc3339: "2026-04-03T12:00:00Z",
      traffic_used_bytes: 0,
      unique_ips_used: 0,
      active_tcp_conns: 0,
      data_quota_bytes: 0,
      last_deploy_status: "failed",
    } as any,
  ], NOW);

  const counts = buildClientFilterCounts(rows);
  const idleRows = filterClientTableRows(rows, { filter: "idle", search: "" });

  assert.equal(counts.all, 3);
  assert.equal(counts.active, 1);
  assert.equal(counts.idle, 1);
  assert.equal(counts.disabled, 1);
  assert.deepEqual(idleRows.map((row) => row.id), ["idle"]);
});

test("sortClientTableRows and paginateClientTableRows keep client ordering deterministic", () => {
  const sorted = sortClientTableRows(
    [
      {
        id: "b",
        clientName: "Beta",
        statusRank: 2,
        connectionsValue: 4,
        serversValue: 1,
        trafficValue: 100,
        quotaValue: 200,
        expiresTimestamp: 3,
      } as any,
      {
        id: "a",
        clientName: "Alpha",
        statusRank: 1,
        connectionsValue: 6,
        serversValue: 2,
        trafficValue: 200,
        quotaValue: 400,
        expiresTimestamp: 1,
      } as any,
      {
        id: "c",
        clientName: "Gamma",
        statusRank: 3,
        connectionsValue: 2,
        serversValue: 3,
        trafficValue: 50,
        quotaValue: 100,
        expiresTimestamp: 2,
      } as any,
    ],
    "client",
    "asc"
  );

  const paged = paginateClientTableRows(sorted, 2, 1);

  assert.deepEqual(sorted.map((row) => row.id), ["a", "b", "c"]);
  assert.equal(paged.totalPages, 3);
  assert.deepEqual(paged.rows.map((row) => row.id), ["b"]);
});
