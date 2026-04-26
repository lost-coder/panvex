import type { FleetGroupOption, UserLinks, ViewMode } from "./common";
import type { ClientAgentOption, ClientFormData } from "./client-form";

// --- Clients ---

export interface ClientListItem {
  id: string;
  name: string;
  enabled: boolean;
  assignedNodesCount: number;
  expirationRfc3339: string;
  trafficUsedBytes: number;
  uniqueIpsUsed: number;
  activeTcpConns: number;
  dataQuotaBytes: number;
  lastDeployStatus: string;
}

/**
 * Bulk operations operators can fire against a multi-selection of
 * clients on /clients. Mirrors `BulkServerAction` pattern from the
 * Servers list. Container translates each value into one or more
 * apiClient calls (updateClient with `enabled` flipped, deleteClient
 * per id).
 */
export type BulkClientAction = "enable" | "disable" | "delete";

export interface ClientsPageProps {
  clients: ClientListItem[];
  viewMode: ViewMode;
  autoThreshold: number;
  onViewModeChange?: ((mode: ViewMode) => void) | undefined;
  onClientClick?: ((clientId: string) => void) | undefined;
  onClientLinkClick?: ((clientId: string) => void) | undefined;
  onCreate?: ((data: ClientFormData) => void | Promise<void>) | undefined;
  createLoading?: boolean | undefined;
  createError?: string | undefined;
  /** Options threaded into the create sheet's deployment selectors. */
  fleetGroups?: FleetGroupOption[] | undefined;
  agents?: ClientAgentOption[] | undefined;
  pendingDiscoveredCount?: number | undefined;
  onDiscoveredClick?: (() => void) | undefined;
  /** Bulk action callback. Container wires it to apiClient calls per id. */
  onBulkAction?: ((action: BulkClientAction, clientIds: string[]) => void | Promise<void>) | undefined;
  bulkError?: string | undefined;
  bulkPending?: boolean | undefined;
}

export interface ClientDeploymentData {
  agentId: string;
  desiredOperation: string;
  status: string;
  lastError: string;
  links: UserLinks;
  lastAppliedAtUnix: number;
}

export interface ClientDetailPageProps {
  client: {
    id: string;
    name: string;
    enabled: boolean;
    secret: string;
    userAdTag: string;
    trafficUsedBytes: number;
    uniqueIpsUsed: number;
    activeTcpConns: number;
    maxTcpConns: number;
    maxUniqueIps: number;
    dataQuotaBytes: number;
    expirationRfc3339: string;
    fleetGroupIds: string[];
    agentIds: string[];
    deployments: ClientDeploymentData[];
  };
  onBack?: (() => void) | undefined;
  onEdit?: ((data: ClientFormData) => void | Promise<void>) | undefined;
  editLoading?: boolean | undefined;
  editError?: string | undefined;
  /** Options threaded into the edit sheet's deployment selectors. */
  fleetGroups?: FleetGroupOption[] | undefined;
  agents?: ClientAgentOption[] | undefined;
  onManageAccess?: (() => void) | undefined;
  onRotateSecret?: (() => void) | undefined;
  secretRotating?: boolean | undefined;
  secretPendingRedeploy?: boolean | undefined;
  /** Retry the rollout of the current stored state to every target
   *  agent. Surfaced as a button when at least one deployment is in
   *  the `failed` state. Container wires it to apiClient.redeployClient. */
  onRedeploy?: (() => void) | undefined;
  redeploying?: boolean | undefined;
  onDisable?: (() => void) | undefined;
  onDelete?: (() => void) | undefined;
  ipHistory?: {
    /**
     * GeoIP fields are optional — the backend does not enrich yet. The
     * page renders "—" placeholders for the column until the enrichment
     * lands.
     */
    ips: {
      ip: string;
      firstSeen: string;
      lastSeen: string;
      countryCode?: string | undefined;
      countryName?: string | undefined;
      city?: string | undefined;
      asn?: string | undefined;
    }[];
    totalUnique: number;
  } | undefined;
  /**
   * Mapping `agent_id → node_name` so the Deployments & Links card can
   * render human-readable names instead of raw UUIDs. Until the backend
   * starts including node_name on clientDeploymentResponse
   * (backend-followup #5), the container joins client-side against
   * /api/agents. Missing ids fall back to the UUID.
   */
  agentLabels?: Record<string, string> | undefined;
}
