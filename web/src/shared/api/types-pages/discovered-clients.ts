import type { UserLinks } from "./common";

// --- Discovered Clients ---

export interface DiscoveredClientConflict {
  type: "same_secret_different_names" | "same_name_different_secrets";
  relatedIds: string[];
}

export interface DiscoveredClientItem {
  id: string;
  agentId: string;
  nodeName: string;
  clientName: string;
  status: "pending_review" | "adopted" | "ignored";
  totalOctets: number;
  currentConnections: number;
  activeUniqueIps: number;
  links: UserLinks;
  maxTcpConns: number;
  maxUniqueIps: number;
  dataQuotaBytes: number;
  expiration: string;
  discoveredAtUnix: number;
  updatedAtUnix: number;
  conflicts?: DiscoveredClientConflict[] | undefined;
}

export interface DiscoveredClientsPageProps {
  clients: DiscoveredClientItem[];
  onAdopt?: ((id: string) => void) | undefined;
  onIgnore?: ((id: string) => void) | undefined;
  onAdoptMany?: ((ids: string[]) => void) | undefined;
  onIgnoreMany?: ((ids: string[]) => void) | undefined;
  onBack?: (() => void) | undefined;
  onRescan?: (() => void) | undefined;
  busy?: boolean | undefined;
  rescanning?: boolean | undefined;
}
