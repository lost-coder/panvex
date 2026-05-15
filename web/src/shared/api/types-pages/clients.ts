import type { FleetGroupOption, UserLinks, ViewMode } from "./common";
import type { ClientAgentOption, ClientFormData } from "./client-form";
// Reset-quota Phase 2: imported via the @/features path so the
// public ClientDetailPageProps stays expressively typed for the
// container without forcing every consumer to also import the hook
// file. The feature still owns the type; this is a one-way contract.
import type { ResetOutcome } from "@/features/clients/hooks/useResetQuota";

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
  /**
   * Reset-quota Phase 1: per-agent quota usage surfaced on the client
   * detail page. Defaults to 0 (transform layer) when the backend
   * omits the field — see schemas/client.ts for the wire-level
   * default and the rationale.
   */
  quotaUsedBytes: number;
  /**
   * Reset-quota Phase 1: unix epoch of the last Telemt-side reset
   * for this (client, agent) pair. 0 means "never reset" (or Telemt
   * predates 3.4.6 / panel still mid-rollout). The UI renders the
   * relative age via `formatAge` and falls back to "Never reset".
   */
  quotaLastResetUnix: number;
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
  /**
   * Reset-quota Phase 2 callback set. The container wires
   * `onResetQuota` to a per-agent confirm dialog + mutation pipeline;
   * passing `undefined` disables the affordance (viewer role).
   * `resetStates` carries the in-flight or terminal outcome for each
   * agent the operator has acted on; the cell pulls its state from
   * here so the parent stays the single source of truth. The "Reset
   * everywhere" header button is wired separately via
   * `onResetQuotaEverywhere`, surfaced only when there are ≥2
   * deployments.
   */
  onResetQuota?: ((agentId: string) => void) | undefined;
  resetStates?: Record<string, ResetOutcome> | undefined;
  onDismissResetState?: ((agentId: string) => void) | undefined;
  onResetQuotaEverywhere?: (() => void) | undefined;
  resetEverywherePending?: boolean | undefined;
}
