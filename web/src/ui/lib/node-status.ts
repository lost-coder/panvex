import type { PillTone, Status } from "@/ui/tokens/colors";

/**
 * Operator-facing node lifecycle state. Plan 1 covers the three states
 * derivable from the legacy 3-state `Status`. Plan 2 extends this union
 * with "offline" | "pending" | "updating" once the data signals + the
 * deferred thresholds are settled.
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
  ok: { tone: "ok", glyph: "✓", labelKey: "fleet.statusOk" },
  degraded: { tone: "warn", glyph: "▲", labelKey: "fleet.statusDegraded" },
  down: { tone: "error", glyph: "⛔", labelKey: "fleet.statusDown" },
  offline: { tone: "error", glyph: "⛔", labelKey: "fleet.statusOffline" },
  pending: { tone: "neutral", glyph: "●", labelKey: "fleet.statusPending" },
};

export function nodeStatePresentation(state: NodeState): NodeStatePresentation {
  return PRESENTATION[state];
}

/** Legacy 3-state Status → NodeState. */
export function nodeStateFromStatus(status: Status): NodeState {
  if (status === "error") return "down";
  if (status === "warn") return "degraded";
  return "ok";
}
