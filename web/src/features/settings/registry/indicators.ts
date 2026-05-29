import type { SchemaEntry, ValuesEntry } from "./types";

export type IndicatorKind =
  | "env-override"
  | "config-managed"
  | "pending-restart"
  | "needs-restart"
  | null;

export type IndicatorTone = "amber" | "grey";

export interface FieldIndicator {
  kind: IndicatorKind;
  bar: IndicatorTone | null; // left accent-bar color, null = no bar
  icon: "lock" | "restart" | null;
  tone: IndicatorTone | null; // icon color (mirrors bar today; kept separate so the two can diverge)
  spinning: boolean; // RefreshCw spins while a restart is pending
  /** Key under settings → registryField.tooltip.* (null = no tooltip). */
  tooltipKey: "envOverride" | "configManaged" | "needsRestart" | "pendingRestart" | null;
}

const NONE: FieldIndicator = {
  kind: null,
  bar: null,
  icon: null,
  tone: null,
  spinning: false,
  tooltipKey: null,
};

// Tailwind v4 token-driven 4px inset accent bar, keyed by tone. Consumed by RegistryField + SettingsLegend.
export const BAR_SHADOW: Record<IndicatorTone, string> = {
  amber: "shadow-[inset_4px_0_0_var(--color-status-warn)]",
  grey: "shadow-[inset_4px_0_0_var(--color-border-hi)]",
};

// First match wins (see spec state table).
export function resolveIndicator(schema: SchemaEntry, values: ValuesEntry): FieldIndicator {
  const apply = values.apply ?? schema.apply;

  if (values.overridden_by_env === true) {
    return { kind: "env-override", bar: "amber", icon: "lock", tone: "amber", spinning: false, tooltipKey: "envOverride" };
  }
  // Any locked field that isn't an env-override is read-only and externally
  // managed (config.toml / CLI / env). One grey "lock" covers them all.
  if (values.locked === true) {
    return { kind: "config-managed", bar: "grey", icon: "lock", tone: "grey", spinning: false, tooltipKey: "configManaged" };
  }
  // pending_value / value arrive as serialised strings from the settings API,
  // so a String() comparison is the intended equality check here.
  if (values.pending_restart === true && String(values.pending_value) !== String(values.value)) {
    return { kind: "pending-restart", bar: "amber", icon: "restart", tone: "amber", spinning: true, tooltipKey: "pendingRestart" };
  }
  if (apply === "restart") {
    return { kind: "needs-restart", bar: "amber", icon: "restart", tone: "amber", spinning: false, tooltipKey: "needsRestart" };
  }
  return NONE;
}
