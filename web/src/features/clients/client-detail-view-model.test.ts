// @ts-nocheck
import test from "node:test";
import assert from "node:assert/strict";

import { buildClientDetailViewModel } from "./client-detail-view-model.ts";

const NOW = Date.UTC(2026, 2, 25, 12, 0, 0);

test("buildClientDetailViewModel maps client detail into the approved read-first sections", () => {
  const viewModel = buildClientDetailViewModel({
    id: "client-1",
    name: "Acme Alpha",
    secret: "telemt-secret-0001",
    user_ad_tag: "ABCDEF0123456789ABCDEF0123456789",
    enabled: true,
    traffic_used_bytes: 1610612736,
    unique_ips_used: 7,
    active_tcp_conns: 12,
    max_tcp_conns: 24,
    max_unique_ips: 10,
    data_quota_bytes: 5368709120,
    expiration_rfc3339: "2026-04-03T12:00:00Z",
    fleet_group_ids: ["eu-edge", "eu-backup"],
    agent_ids: ["agent-1", "agent-2"],
    deployments: [
      {
        agent_id: "agent-fra-01",
        desired_operation: "update",
        status: "failed",
        last_error: "telemt apply failed",
        connection_link: "",
        last_applied_at_unix: 0,
        updated_at_unix: 1711365300,
      },
      {
        agent_id: "agent-ams-02",
        desired_operation: "update",
        status: "succeeded",
        last_error: "",
        connection_link: "https://example.test/link",
        last_applied_at_unix: 1711365000,
        updated_at_unix: 1711365100,
      },
      {
        agent_id: "agent-waw-03",
        desired_operation: "update",
        status: "pending",
        last_error: "",
        connection_link: "",
        last_applied_at_unix: 0,
        updated_at_unix: 1711365200,
      },
    ],
    created_at_unix: 1711362000,
    updated_at_unix: 1711365600,
    deleted_at_unix: 0,
  } as any, { nowMs: NOW });

  assert.equal(viewModel.header.nameText, "Acme Alpha");
  assert.equal(viewModel.header.statusText, "Active");
  assert.equal(viewModel.header.statusTone, "good");
  assert.equal(viewModel.header.deploymentText, "1 failed, 1 pending");
  assert.equal(viewModel.header.deploymentTone, "bad");
  assert.equal(viewModel.overviewStats[0]?.label, "Traffic");
  assert.equal(viewModel.overviewStats[0]?.valueText, "1.5 GB");
  assert.equal(viewModel.overviewStats[1]?.valueText, "12");
  assert.equal(viewModel.overviewStats[2]?.valueText, "3");
  assert.equal(viewModel.overviewStats[3]?.valueText, "1.5 GB / 5.0 GB");
  assert.equal(viewModel.overviewStats[4]?.valueText, "in 9d");
  assert.equal(viewModel.identityItems[2]?.valueText, "ABCDEF0123456789ABCDEF0123456789");
  assert.equal(viewModel.identitySecret.maskedText, "telemt-se...0001");
  assert.equal(viewModel.usageItems[1]?.valueText, "7");
  assert.equal(viewModel.limitItems[0]?.valueText, "24");
  assert.equal(viewModel.assignmentGroups[0], "eu-edge");
  assert.equal(viewModel.assignmentAgents[0], "agent-1");
  assert.equal(viewModel.assignmentSummaryText, "2 fleet groups, 2 explicit nodes");
  assert.equal(viewModel.deploymentRows[0]?.agentText, "agent-fra-01");
  assert.equal(viewModel.deploymentRows[0]?.statusText, "Failed");
  assert.equal(viewModel.deploymentRows[0]?.statusTone, "bad");
  assert.equal(
    viewModel.deploymentRows.find((row) => row.linkText !== "—")?.linkText,
    "https://example.test/link"
  );
});

test("buildClientDetailViewModel keeps idle and empty sections stable when rollout targets are missing", () => {
  const viewModel = buildClientDetailViewModel({
    id: "client-2",
    name: "Dormant Client",
    secret: "plain-secret",
    user_ad_tag: "",
    enabled: true,
    traffic_used_bytes: 0,
    unique_ips_used: 0,
    active_tcp_conns: 0,
    max_tcp_conns: 0,
    max_unique_ips: 0,
    data_quota_bytes: 0,
    expiration_rfc3339: "",
    fleet_group_ids: [],
    agent_ids: [],
    deployments: [],
    created_at_unix: 0,
    updated_at_unix: 0,
    deleted_at_unix: 0,
  } as any, { nowMs: NOW });

  assert.equal(viewModel.header.statusText, "Idle");
  assert.equal(viewModel.header.statusTone, "warn");
  assert.equal(viewModel.header.deploymentText, "No rollout targets");
  assert.equal(viewModel.assignmentSummaryText, "No rollout targets configured");
  assert.equal(viewModel.deploymentRows.length, 0);
  assert.equal(viewModel.identityItems[2]?.valueText, "—");
  assert.equal(viewModel.overviewStats[4]?.secondaryText, "No expiration configured");
});

test("buildClientDetailViewModel keeps configured assignments separate from rollout targets that are not materialized yet", () => {
  const viewModel = buildClientDetailViewModel({
    id: "client-assignments",
    name: "Assigned Client",
    secret: "secret",
    user_ad_tag: "tag",
    enabled: true,
    traffic_used_bytes: 0,
    unique_ips_used: 0,
    active_tcp_conns: 0,
    max_tcp_conns: 0,
    max_unique_ips: 0,
    data_quota_bytes: 0,
    expiration_rfc3339: "",
    fleet_group_ids: ["eu-edge"],
    agent_ids: ["agent-1"],
    deployments: [],
    created_at_unix: 0,
    updated_at_unix: 0,
    deleted_at_unix: 0,
  } as any, { nowMs: NOW });

  assert.equal(viewModel.header.deploymentText, "Targets pending materialization");
  assert.equal(viewModel.header.deploymentTone, "warn");
  assert.equal(viewModel.header.metaItems[1]?.valueText, "1 fleet group, 1 explicit node");
  assert.equal(viewModel.overviewStats[2]?.valueText, "Pending");
  assert.equal(
    viewModel.overviewStats[2]?.secondaryText,
    "Assignments configured, rollout targets not reported yet"
  );
});

test("buildClientDetailViewModel formats near-boundary expirations without misleading zero-day text", () => {
  const viewModel = buildClientDetailViewModel({
    id: "client-exp",
    name: "Expiring Client",
    secret: "secret",
    user_ad_tag: "tag",
    enabled: true,
    traffic_used_bytes: 0,
    unique_ips_used: 0,
    active_tcp_conns: 0,
    max_tcp_conns: 0,
    max_unique_ips: 0,
    data_quota_bytes: 0,
    expiration_rfc3339: "2026-03-25T15:00:00Z",
    fleet_group_ids: [],
    agent_ids: [],
    deployments: [],
    created_at_unix: 0,
    updated_at_unix: 0,
    deleted_at_unix: 0,
  } as any, { nowMs: NOW });

  assert.equal(viewModel.overviewStats[4]?.valueText, "in 3h");
});

test("buildClientDetailViewModel marks disabled clients as disabled regardless of live connections", () => {
  const viewModel = buildClientDetailViewModel({
    id: "client-3",
    name: "Disabled Client",
    secret: "secret",
    user_ad_tag: "tag",
    enabled: false,
    traffic_used_bytes: 100,
    unique_ips_used: 1,
    active_tcp_conns: 5,
    max_tcp_conns: 10,
    max_unique_ips: 2,
    data_quota_bytes: 500,
    expiration_rfc3339: "2026-03-30T12:00:00Z",
    fleet_group_ids: [],
    agent_ids: [],
    deployments: [],
    created_at_unix: 0,
    updated_at_unix: 0,
    deleted_at_unix: 0,
  } as any, { nowMs: NOW });

  assert.equal(viewModel.header.statusText, "Disabled");
  assert.equal(viewModel.header.statusTone, "bad");
});
