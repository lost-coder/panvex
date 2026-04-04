import type { ClientListItem, ClientDetailPageProps } from "@panvex/ui";
import type {
  ClientListItem as ApiClientListItem,
  Client as ApiClient,
} from "../api";

export function transformClientList(
  raw: ApiClientListItem[]
): ClientListItem[] {
  return (raw ?? []).map((c) => ({
    id: c.id,
    name: c.name,
    enabled: c.enabled,
    assignedNodesCount: c.assigned_nodes_count,
    expirationRfc3339: c.expiration_rfc3339,
    trafficUsedBytes: c.traffic_used_bytes,
    uniqueIpsUsed: c.unique_ips_used,
    activeTcpConns: c.active_tcp_conns,
    dataQuotaBytes: c.data_quota_bytes,
    lastDeployStatus: c.last_deploy_status,
  }));
}

export function transformClientDetail(
  raw: ApiClient
): ClientDetailPageProps["client"] {
  return {
    id: raw.id,
    name: raw.name,
    enabled: raw.enabled,
    secret: raw.secret,
    userAdTag: raw.user_ad_tag,
    trafficUsedBytes: raw.traffic_used_bytes,
    uniqueIpsUsed: raw.unique_ips_used,
    activeTcpConns: raw.active_tcp_conns,
    maxTcpConns: raw.max_tcp_conns,
    maxUniqueIps: raw.max_unique_ips,
    dataQuotaBytes: raw.data_quota_bytes,
    expirationRfc3339: raw.expiration_rfc3339,
    fleetGroupIds: raw.fleet_group_ids ?? [],
    deployments: (raw.deployments ?? []).map((d) => ({
      agentId: d.agent_id,
      desiredOperation: d.desired_operation,
      status: d.status,
      lastError: d.last_error,
      connectionLink: d.connection_link,
      lastAppliedAtUnix: d.last_applied_at_unix,
    })),
  };
}
