import type { PillTone } from "@/ui/tokens/colors";

/**
 * Operator-facing node lifecycle state. Five-state union: `offline` (no
 * heartbeat) and `down` (agent running, traffic not served) are distinct red
 * states; `pending` is the neutral startup state. Update-availability is
 * surfaced separately (info chip), not as a state here.
 */
export type NodeState = "ok" | "degraded" | "down" | "offline" | "pending";

export interface NodeStatePresentation {
  /** Severity tone, reused by StatusPill / status color classes. */
  tone: PillTone;
  /** Shape glyph so state survives color-blindness (not color-only). */
  glyph: string;
  /** i18n key — caller resolves via t(); keeps copy translatable. */
  labelKey: string;
}

const PRESENTATION: Record<NodeState, NodeStatePresentation> = {
  ok: { tone: "ok", glyph: "✓", labelKey: "status.ok" },
  degraded: { tone: "warn", glyph: "▲", labelKey: "status.degraded" },
  down: { tone: "error", glyph: "⛔", labelKey: "status.down" },
  offline: { tone: "error", glyph: "⛔", labelKey: "status.offline" },
  pending: { tone: "neutral", glyph: "●", labelKey: "status.pending" },
};

export function nodeStatePresentation(state: NodeState): NodeStatePresentation {
  return PRESENTATION[state];
}

/**
 * Stable backend reason strings that mean "the node is still coming up"
 * (telemetry/projections.go: SeverityAndReason). These map to the neutral
 * PENDING state instead of amber DEGRADED so a planned startup doesn't read
 * as a fire. SEAM: if the backend changes these strings or adds a dedicated
 * startup field, update this list (or switch to the field).
 */
const STARTUP_REASONS = ["Startup is still in progress"];

export function isStartupReason(reason: string): boolean {
  return STARTUP_REASONS.includes(reason.trim());
}

export interface NodeStateInput {
  /** Raw backend severity (accepts legacy "good"). */
  severity: "good" | "ok" | "warn" | "critical" | "bad";
  /** Agent presence: "online" | "degraded" | "offline" (string for forward-compat). */
  presenceState: string;
  telemtUnreachable: boolean;
  /** Backend human reason; used to detect the startup/pending case. */
  reason: string;
}

/** Map backend per-node signals to a NodeState. Priority: offline > down > pending > degraded > ok. */
export function deriveNodeState(input: NodeStateInput): NodeState {
  if (input.presenceState === "offline") return "offline";
  if (input.telemtUnreachable || input.severity === "critical" || input.severity === "bad") {
    return "down";
  }
  if (isStartupReason(input.reason)) return "pending";
  if (input.severity === "warn") return "degraded";
  return "ok";
}
