import type { ClientListItem, ClientDetailPageProps, ClientFormData } from "@lost-coder/panvex-ui";
import type {
  ClientListItem as ApiClientListItem,
  Client as ApiClient,
  ClientInput,
} from "../api";

/** Parse a single connection_link string into categorized link arrays. */
export function parseConnectionLink(link: string): { classic: string[]; secure: string[]; tls: string[] } {
  if (!link) return { classic: [], secure: [], tls: [] };
  if (link.startsWith("https://t.me/")) return { classic: [link], secure: [], tls: [] };
  if (link.startsWith("tg://proxy")) {
    const match = link.match(/secret=([0-9a-fA-F]+)/);
    const secret = match?.[1];
    if (secret && secret.toLowerCase().startsWith("ee")) {
      return { classic: [], secure: [], tls: [link] };
    }
    return { classic: [], secure: [link], tls: [] };
  }
  return { classic: [], secure: [link], tls: [] };
}

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
      links: parseConnectionLink(d.connection_link),
      lastAppliedAtUnix: d.last_applied_at_unix,
    })),
  };
}

/** Convert ClientFormData back to API ClientInput, preserving fields not in the form. */
export function buildClientInput(form: ClientFormData, existing: ApiClient): ClientInput {
  return {
    name: form.name,
    enabled: existing.enabled,
    user_ad_tag: form.userAdTag,
    max_tcp_conns: form.maxTcpConns,
    max_unique_ips: form.maxUniqueIps,
    data_quota_bytes: form.dataQuotaBytes,
    expiration_rfc3339: form.expirationRfc3339,
    fleet_group_ids: existing.fleet_group_ids ?? [],
    agent_ids: existing.agent_ids ?? [],
  };
}
