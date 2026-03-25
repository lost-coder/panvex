// @ts-nocheck
import test from "node:test";
import assert from "node:assert/strict";

import {
  buildServerFilterCounts,
  buildServerTableRows,
  filterServerTableRows,
  paginateServerTableRows,
  sortServerTableRows,
} from "./servers-view-model.ts";

test("buildServerTableRows maps agents into display rows with placeholders and dc summaries", () => {
  const rows = buildServerTableRows([
    {
      id: "agent-1",
      node_name: "de-fra-01",
      fleet_group_id: "core-eu",
      presence_state: "online",
      version: "1.2.3",
      read_only: false,
      last_seen_at: "2026-03-24T10:00:00Z",
      runtime: {
        degraded: false,
        accepting_new_connections: true,
        uptime_seconds: 90061,
        active_users: 342,
        current_connections: 400,
        dc_coverage_pct: 100,
        healthy_upstreams: 2,
        total_upstreams: 2,
        dcs: [
          {
            dc: 1,
            available_endpoints: 1,
            available_pct: 100,
            required_writers: 3,
            alive_writers: 3,
            coverage_pct: 100,
            rtt_ms: 18,
            load: 1,
          },
          {
            dc: 2,
            available_endpoints: 1,
            available_pct: 66,
            required_writers: 3,
            alive_writers: 2,
            coverage_pct: 66,
            rtt_ms: 120,
            load: 2,
          },
          {
            dc: 3,
            available_endpoints: 0,
            available_pct: 0,
            required_writers: 3,
            alive_writers: 0,
            coverage_pct: 0,
            rtt_ms: 500,
            load: 3,
          },
        ],
      },
    } as any,
  ]);

  assert.equal(rows[0]?.serverName, "de-fra-01");
  assert.equal(rows[0]?.groupText, "core-eu");
  assert.equal(rows[0]?.clientsText, "342");
  assert.equal(rows[0]?.cpuText, "—");
  assert.equal(rows[0]?.memoryText, "—");
  assert.equal(rows[0]?.trafficText, "—");
  assert.equal(rows[0]?.uptimeText, "1d 1h");
  assert.equal(rows[0]?.dcSummaryText, "2/3");
  assert.deepEqual(rows[0]?.dcSegments, ["ok", "partial", "down"]);
});

test("buildServerFilterCounts and filterServerTableRows treat degraded and offline nodes as issues", () => {
  const rows = buildServerTableRows([
    {
      id: "healthy",
      node_name: "healthy",
      fleet_group_id: "",
      presence_state: "online",
      version: "1",
      read_only: false,
      last_seen_at: "2026-03-24T10:00:00Z",
      runtime: {
        degraded: false,
        accepting_new_connections: true,
        active_users: 10,
        current_connections: 10,
        dc_coverage_pct: 100,
        healthy_upstreams: 2,
        total_upstreams: 2,
        dcs: [],
      },
    } as any,
    {
      id: "degraded",
      node_name: "degraded",
      fleet_group_id: "",
      presence_state: "online",
      version: "1",
      read_only: false,
      last_seen_at: "2026-03-24T10:00:00Z",
      runtime: {
        degraded: true,
        accepting_new_connections: true,
        active_users: 10,
        current_connections: 10,
        dc_coverage_pct: 100,
        healthy_upstreams: 2,
        total_upstreams: 2,
        dcs: [],
      },
    } as any,
    {
      id: "offline",
      node_name: "offline",
      fleet_group_id: "",
      presence_state: "offline",
      version: "1",
      read_only: false,
      last_seen_at: "2026-03-24T10:00:00Z",
      runtime: {
        degraded: false,
        accepting_new_connections: false,
        active_users: 0,
        current_connections: 0,
        dc_coverage_pct: 0,
        healthy_upstreams: 0,
        total_upstreams: 0,
        dcs: [],
      },
    } as any,
  ]);

  const counts = buildServerFilterCounts(rows);
  const issues = filterServerTableRows(rows, { filter: "issues", search: "" });

  assert.equal(counts.all, 3);
  assert.equal(counts.online, 1);
  assert.equal(counts.issues, 2);
  assert.equal(counts.offline, 1);
  assert.deepEqual(issues.map((row) => row.id), ["degraded", "offline"]);
});

test("sortServerTableRows and paginateServerTableRows keep server ordering deterministic", () => {
  const sorted = sortServerTableRows(
    [
      { id: "b", serverName: "beta", severity: 1, clientsValue: 5, dcAvailableCount: 3, dcTotalCount: 4 } as any,
      { id: "a", serverName: "alpha", severity: 3, clientsValue: 12, dcAvailableCount: 0, dcTotalCount: 4 } as any,
      { id: "c", serverName: "gamma", severity: 2, clientsValue: 8, dcAvailableCount: 2, dcTotalCount: 4 } as any,
    ],
    "server",
    "asc"
  );

  const paged = paginateServerTableRows(sorted, 2, 1);

  assert.deepEqual(sorted.map((row) => row.id), ["a", "b", "c"]);
  assert.equal(paged.totalPages, 3);
  assert.deepEqual(paged.rows.map((row) => row.id), ["b"]);
});
