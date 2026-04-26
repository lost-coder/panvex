import type { FleetGroupOption } from "./common";

// --- Enrollment ---

export interface EnrollmentWizardProps {
  step: 1 | 2 | 3;
  // Step 1
  fleetGroups: FleetGroupOption[];
  nodeName: string;
  selectedFleetGroup: string;
  tokenTtl: number;
  onNodeNameChange: (name: string) => void;
  onFleetGroupChange: (id: string) => void;
  /** Optional inline-create hook. When provided, the wizard renders
   *  a "+ New group" button next to the select that opens a mini
   *  dialog owned by the container. */
  onCreateFleetGroup?: (() => void) | undefined;
  onTokenTtlChange: (seconds: number) => void;
  onGenerateToken: () => void;
  // Step 2
  installCommand: string;
  tokenValue: string;
  tokenExpiresInSecs: number;
  advancedOptions?: {
    telemtUrl: string;
    telemtMetricsUrl: string;
    telemtAuth: string;
    /** Pass `--insecure-transport` to the bootstrap command. Use only on
     *  trusted private links (VPN-only / internal network) where the panel
     *  runs plain HTTP and TLS is terminated elsewhere or not at all. */
    insecureTransport: boolean;
  } | undefined;
  onAdvancedOptionsChange?: ((opts: {
    telemtUrl: string;
    telemtMetricsUrl: string;
    telemtAuth: string;
    insecureTransport: boolean;
  }) => void) | undefined;
  onInstallConfirm: () => void;
  onBack: () => void;
  // Step 3
  connectionStatus: {
    // All three stages share the same state machine now: pending →
    // waiting (in progress) → done. Bootstrap used to be modelled as a
    // binary flip the moment the operator hit "I've run the command",
    // but the wizard actually waits for the backend to confirm token
    // consumption, so it needs the full progression.
    bootstrap: "pending" | "waiting" | "done";
    grpcConnect: "pending" | "waiting" | "done";
    firstData: "pending" | "waiting" | "done";
  };
  connectedAgent?: {
    id: string;
    version: string;
    fleetGroup: string;
    certExpiresAt: string;
  } | undefined;
  onViewDetails: () => void;
  onCancel: () => void;
  // Shared
  loading?: boolean | undefined;
  error?: string | undefined;
}

export interface EnrollmentTokenData {
  value: string;
  fleetGroupId: string;
  status: "active" | "consumed" | "expired" | "revoked";
  issuedAtUnix: number;
  expiresAtUnix: number;
}

export interface TokenListProps {
  tokens: EnrollmentTokenData[];
  onRevoke: (tokenValue: string) => void;
  /** Snapshot of current unix seconds, used to render TTL countdowns. */
  nowSec?: number | undefined;
}

// --- Enrollment Tokens Page ---

export interface EnrollmentTokensPageProps {
  tokens: EnrollmentTokenData[];
  onCreateToken: () => void;
  onRevoke: (tokenValue: string) => void;
}
