import { describe, expect, it } from "vitest";
import {
  parseConnectionLink,
  transformClientList,
  transformClientDetail,
  buildClientInput,
} from "./clients";
import type { Client as ApiClient } from "@/lib/api";

describe("parseConnectionLink", () => {
  it("returns empty buckets for empty input", () => {
    expect(parseConnectionLink("")).toEqual({ classic: [], secure: [], tls: [] });
  });

  it("classifies https://t.me/ links as classic", () => {
    const r = parseConnectionLink("https://t.me/proxy?server=x");
    expect(r.classic).toHaveLength(1);
    expect(r.secure).toHaveLength(0);
    expect(r.tls).toHaveLength(0);
  });

  it("classifies tg://proxy with ee-prefixed secret as TLS", () => {
    const r = parseConnectionLink("tg://proxy?server=x&port=443&secret=eeABCDEF");
    expect(r.tls).toHaveLength(1);
    expect(r.secure).toHaveLength(0);
  });

  it("classifies tg://proxy with non-ee secret as secure", () => {
    const r = parseConnectionLink("tg://proxy?server=x&port=443&secret=dd1122");
    expect(r.secure).toHaveLength(1);
    expect(r.tls).toHaveLength(0);
  });

  it("treats unknown scheme as secure fallback", () => {
    const r = parseConnectionLink("something-else");
    expect(r.secure).toEqual(["something-else"]);
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
      connection_link: "tg://proxy?server=x&port=443&secret=ee1234",
      last_applied_at_unix: 1_700_000_000,
      updated_at_unix: 1_700_000_100,
    },
  ],
  created_at_unix: 1_700_000_000,
  updated_at_unix: 1_700_000_100,
  deleted_at_unix: 0,
};

describe("transformClientDetail", () => {
  it("maps deployments + parses connection_link", () => {
    const r = transformClientDetail(rawClient);
    expect(r.name).toBe("alpha");
    expect(r.fleetGroupIds).toEqual(["g1"]);
    expect(r.deployments).toHaveLength(1);
    expect(r.deployments[0].agentId).toBe("a1");
    expect(r.deployments[0].links.tls).toHaveLength(1);
  });
});

describe("buildClientInput", () => {
  it("preserves enabled/fleet/agent fields from the existing client", () => {
    const input = buildClientInput(
      {
        name: "alpha-v2",
        userAdTag: "tag2",
        maxTcpConns: 50,
        maxUniqueIps: 5,
        dataQuotaBytes: 4096,
        expirationRfc3339: "2031-01-01T00:00:00Z",
      } as unknown as Parameters<typeof buildClientInput>[0],
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
      fleet_group_ids: ["g1"],
      agent_ids: ["a1"],
    });
  });
});
