import { describe, expect, it } from "vitest";
import {
  categorizeConnectionLinks,
  transformClientList,
  transformClientDetail,
  buildClientInput,
} from "./clients";
import type { Client as ApiClient } from "@/shared/api/api";

describe("categorizeConnectionLinks", () => {
  it("returns empty buckets for empty input", () => {
    expect(categorizeConnectionLinks([])).toEqual({ classic: [], secure: [], tls: [] });
  });

  it("classifies https://t.me/ links as classic", () => {
    const r = categorizeConnectionLinks(["https://t.me/proxy?server=x"]);
    expect(r.classic).toHaveLength(1);
    expect(r.secure).toHaveLength(0);
    expect(r.tls).toHaveLength(0);
  });

  it("classifies tg://proxy with ee-prefixed secret as TLS", () => {
    const r = categorizeConnectionLinks(["tg://proxy?server=x&port=443&secret=eeABCDEF"]);
    expect(r.tls).toHaveLength(1);
    expect(r.secure).toHaveLength(0);
  });

  it("classifies tg://proxy with non-ee secret as secure", () => {
    const r = categorizeConnectionLinks(["tg://proxy?server=x&port=443&secret=dd1122"]);
    expect(r.secure).toHaveLength(1);
    expect(r.tls).toHaveLength(0);
  });

  it("treats unknown scheme as secure fallback", () => {
    const r = categorizeConnectionLinks(["something-else"]);
    expect(r.secure).toEqual(["something-else"]);
  });

  it("buckets a multi-domain TLS list across all domains", () => {
    const r = categorizeConnectionLinks([
      "tg://proxy?server=x&port=443&secret=eeAAAA1111",
      "tg://proxy?server=y&port=443&secret=eeBBBB2222",
      "tg://proxy?server=x&port=443&secret=dd9999",
    ]);
    expect(r.tls).toHaveLength(2);
    expect(r.secure).toHaveLength(1);
    expect(r.classic).toHaveLength(0);
  });

  it("skips empty/whitespace entries", () => {
    const r = categorizeConnectionLinks(["", "  ", "tg://proxy?server=x&port=443&secret=ee01"]);
    expect(r.tls).toEqual(["tg://proxy?server=x&port=443&secret=ee01"]);
  });
});

describe("transformClientList", () => {
  it("maps snake_case API fields to camelCase UI fields", () => {
    const result = transformClientList([
      {
        id: "c1",
        name: "alpha",
        enabled: true,
        assigned_nodes_count: 4,
        expiration_rfc3339: "2030-01-01T00:00:00Z",
        traffic_used_bytes: 512,
        unique_ips_used: 2,
        active_tcp_conns: 1,
        data_quota_bytes: 1024,
        last_deploy_status: "applied",
      },
    ]);
    expect(result).toEqual([
      {
        id: "c1",
        name: "alpha",
        enabled: true,
        assignedNodesCount: 4,
        expirationRfc3339: "2030-01-01T00:00:00Z",
        trafficUsedBytes: 512,
        uniqueIpsUsed: 2,
        activeTcpConns: 1,
        dataQuotaBytes: 1024,
        lastDeployStatus: "applied",
      },
    ]);
  });

  it("returns [] for null/undefined", () => {
    expect(
      transformClientList(undefined as unknown as Parameters<typeof transformClientList>[0]),
    ).toEqual([]);
  });
});

const rawClient: ApiClient = {
  id: "c1",
  name: "alpha",
  enabled: true,
  secret: "dd00",
  user_ad_tag: "tag",
  traffic_used_bytes: 10,
  unique_ips_used: 1,
  active_tcp_conns: 1,
  max_tcp_conns: 100,
  max_unique_ips: 10,
  data_quota_bytes: 2048,
  expiration_rfc3339: "2030-01-01T00:00:00Z",
  fleet_group_ids: ["g1"],
  agent_ids: ["a1"],
  deployments: [
    {
      agent_id: "a1",
      desired_operation: "apply",
      status: "ok",
      last_error: "",
      connection_links: [
        "tg://proxy?server=x&port=443&secret=ee1234",
        "tg://proxy?server=y&port=443&secret=ee5678",
      ],
      last_applied_at_unix: 1_700_000_000,
      updated_at_unix: 1_700_000_100,
    },
  ],
  created_at_unix: 1_700_000_000,
  updated_at_unix: 1_700_000_100,
  deleted_at_unix: 0,
};

describe("transformClientDetail", () => {
  it("maps deployments + categorizes every connection link", () => {
    const r = transformClientDetail(rawClient);
    expect(r.name).toBe("alpha");
    expect(r.fleetGroupIds).toEqual(["g1"]);
    expect(r.deployments).toHaveLength(1);
    expect(r.deployments[0]!.agentId).toBe("a1");
    // Two TLS links from a multi-domain Telemt config land in the
    // tls bucket together.
    expect(r.deployments[0]!.links.tls).toHaveLength(2);
  });

  it("maps snake_case quota_used_bytes/quota_last_reset_unix to camelCase", () => {
    // Reset-quota Phase 1: the per-agent quota fields ride on the
    // deployment row. When the backend supplies them, the transform
    // must surface the value verbatim.
    const r = transformClientDetail({
      ...rawClient,
      deployments: [
        {
          ...rawClient.deployments[0]!,
          quota_used_bytes: 524_288_000,
          quota_last_reset_unix: 1_700_000_500,
        },
      ],
    });
    expect(r.deployments[0]!.quotaUsedBytes).toBe(524_288_000);
    expect(r.deployments[0]!.quotaLastResetUnix).toBe(1_700_000_500);
  });

  it("defaults quotaUsedBytes/quotaLastResetUnix to 0 when fields are absent", () => {
    // Backend pre-Phase-1 (or Telemt < 3.4.6) omits the two fields
    // entirely. The transform must coerce undefined to 0 so the UI
    // never receives NaN/undefined math downstream.
    const r = transformClientDetail(rawClient);
    expect(r.deployments[0]!.quotaUsedBytes).toBe(0);
    expect(r.deployments[0]!.quotaLastResetUnix).toBe(0);
  });
});

describe("buildClientInput", () => {
  it("takes enabled from the existing client and deployment targets from the form", () => {
    // The deployment selectors in ClientFormSheet write directly into
    // ClientFormData, so buildClientInput now pulls fleet_group_ids /
    // agent_ids from the form payload rather than the stored client.
    // `enabled` still comes from `existing` because the form never
    // exposes it — toggling is handled out-of-band.
    const input = buildClientInput(
      {
        name: "alpha-v2",
        userAdTag: "tag2",
        userAdTagAuto: false,
        maxTcpConns: 50,
        maxUniqueIps: 5,
        dataQuotaBytes: 4096,
        expirationRfc3339: "2031-01-01T00:00:00Z",
        fleetGroupIds: ["g1", "g2"],
        agentIds: ["a1"],
      },
      rawClient,
    );

    expect(input).toMatchObject({
      name: "alpha-v2",
      enabled: true,
      user_ad_tag: "tag2",
      max_tcp_conns: 50,
      max_unique_ips: 5,
      data_quota_bytes: 4096,
      expiration_rfc3339: "2031-01-01T00:00:00Z",
      fleet_group_ids: ["g1", "g2"],
      agent_ids: ["a1"],
    });
  });
});
