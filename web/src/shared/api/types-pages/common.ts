import type { Status } from "@/ui/tokens/colors";

// Cross-domain primitives shared by multiple page-level type modules.

/** Proxy link URLs grouped by protocol variant. */
export interface UserLinks {
  classic: string[];
  secure: string[];
  tls: string[];
}

/** @deprecated Use `Status` from `@/tokens/colors` instead */
export type Severity = Status;

export interface KpiItem {
  label: string;
  value: string;
  sub?: string | undefined;
  accent?: boolean | undefined;
  /** Sparkline data — one point per sample; omit to render without a chart. */
  series?: number[] | undefined;
  /** Human-readable delta ("+4.2% · 24h"); rendered under the value. */
  deltaLabel?: string | undefined;
  /** Controls the arrow glyph rendered next to `deltaLabel`. */
  deltaDirection?: "up" | "down" | "flat" | undefined;
  /** Coloring the value + sparkline — use for health signals, not trend. */
  tone?: "default" | "ok" | "warn" | "error" | undefined;
}

export interface TrendItem {
  label: string;
  data: number[];
  color: string;
  current: string;
}

export interface TimelineEventData {
  status: Status | "info";
  time: string;
  message: string;
  /** Originating node name — shown on a first row above the message. */
  source?: string | undefined;
}

export interface AlertData {
  severity: "warn" | "crit";
  message: string;
  source: string;
  timestamp: string;
}

export type ViewMode = "cards" | "list";

// --- Fleet groups (used by dashboard, servers, clients, enrollment, client form) ---

export interface FleetGroupOption {
  id: string;
  name?: string | undefined;
  label?: string | undefined;
  nodeCount?: number | undefined;
  agentCount?: number | undefined;
}

// --- Agent Connection (used by server-detail) ---

export interface AgentConnectionData {
  presenceState: "online" | "degraded" | "offline";
  lastSeenAt: string;
  agentId: string;
  version: string;
  fleetGroup: string;
  certificate: {
    issuedAt: string;
    expiresAt: string;
    remainingDays: number;
  };
  recoveryGrant?: {
    status: "allowed" | "used" | "revoked";
    expiresAtUnix: number;
  } | undefined;
  /** Latest available agent version. When set and differs from `version`, shows an update indicator. */
  latestAgentVersion?: string | undefined;
  /** Called when the user clicks the "Update" button. */
  onUpdate?: (() => void) | undefined;
}

export interface AgentConnectionSectionProps {
  data: AgentConnectionData;
  onAllowReEnrollment: () => void;
  onRevokeGrant: () => void;
}

// --- Init State (used by server-detail) ---

export interface InitCardProps {
  stage: string;
  progressPct: number;
  attempt: number;
  retryLimit: number;
  elapsedSecs: number;
  lastError?: string | undefined;
  degraded?: boolean | undefined;
}
