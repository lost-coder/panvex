import type { DiscoveredClientItem } from "@panvex/ui";
import type { DiscoveredClient as ApiDiscoveredClient } from "../api";

export function transformDiscoveredClientList(
  raw: ApiDiscoveredClient[]
): DiscoveredClientItem[] {
  return (raw ?? []).map((dc) => ({
    id: dc.id,
    agentId: dc.agent_id,
    nodeName: dc.node_name,
    clientName: dc.client_name,
    status: dc.status,
    totalOctets: dc.total_octets,
    currentConnections: dc.current_connections,
    activeUniqueIps: dc.active_unique_ips,
    connectionLink: dc.connection_link,
    maxTcpConns: dc.max_tcp_conns,
    maxUniqueIps: dc.max_unique_ips,
    dataQuotaBytes: dc.data_quota_bytes,
    expiration: dc.expiration,
    discoveredAtUnix: dc.discovered_at_unix,
    updatedAtUnix: dc.updated_at_unix,
    conflicts: dc.conflicts?.map((c) => ({
      type: c.type,
      relatedIds: c.related_ids,
    })),
  }));
}
