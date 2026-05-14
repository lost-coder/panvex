import type { FleetGroupOption } from "./common";

// --- Enrollment ---

export type EnrollmentStage = "pending" | "waiting" | "done";

/** Wire-level transport mode. Mirrors the agents.transport_mode column
 *  and the OpenAPI enum. "inbound" = agent dials panel (default);
 *  "outbound" = panel dials agent on the provided dial_address. */
export type EnrollmentMode = "inbound" | "outbound";

/** Install-script source the wizard renders into the curl. Inbound
 *  defaults to "panel" (panel-served with SHA-256 self-check); outbound
 *  defaults to "github" because the panel is typically firewalled from
 *  the agent host. The toggle is only rendered when the container
 *  forwards both `scriptSourcePanelAvailable=true` (a panel URL + hash
 *  is known) and an `onScriptSourceChange` callback. */
export type ScriptSourceKind = "panel" | "github";

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
  // Step 1 — transport mode + outbound fields. Optional so existing
  // callers (and the existing test fixtures) continue to render the
  // inbound-only flow without changes; when `mode` and `onModeChange`
  // are both provided the wizard renders a mode picker above the form.
  mode?: EnrollmentMode | undefined;
  onModeChange?: ((mode: EnrollmentMode) => void) | undefined;
  /** Public host:port the panel dials when mode === "outbound". Required
   *  in that branch; the field renders only when the picker resolves to
   *  outbound. */
  dialAddress?: string | undefined;
  onDialAddressChange?: ((addr: string) => void) | undefined;
  /** Install-script source toggle state (Advanced section). The toggle
   *  is rendered iff both `scriptSource` and `onScriptSourceChange` are
   *  provided; Panel is always selectable because the container can
   *  always derive `<panel>/install-agent.sh` from the panel URL even
   *  when the backend's `script_sources` payload is absent (legacy). */
  scriptSource?: ScriptSourceKind | undefined;
  onScriptSourceChange?: ((src: ScriptSourceKind) => void) | undefined;
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
    bootstrap: EnrollmentStage;
    grpcConnect: EnrollmentStage;
    firstData: EnrollmentStage;
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
