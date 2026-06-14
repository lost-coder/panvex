// Curated editable Telemt config fields for the panel. applyMode is sourced from
// docs/telemt-config-param-catalog.md (hot = applied live by Telemt's file
// watcher; restart = needs a Telemt process restart). The set is intentionally
// small for v1; extend CONFIG_FIELDS as more knobs are surfaced. Editable
// sections only: general, timeouts, censorship, upstreams, show_link, dc_overrides.
export type ApplyMode = "hot" | "restart";
export type FieldType = "string" | "number" | "boolean" | "string[]" | "select";

export interface ConfigField {
  path: string; // "section.key"
  section: string;
  key: string;
  labelKey: string; // i18n key
  type: FieldType;
  applyMode: ApplyMode;
  options?: string[]; // for select
}

export const CONFIG_FIELDS: ConfigField[] = [
  { path: "general.log_level", section: "general", key: "log_level", labelKey: "config.field.log_level", type: "select", applyMode: "hot", options: ["error", "warn", "info", "debug", "trace"] },
  { path: "general.update_every", section: "general", key: "update_every", labelKey: "config.field.update_every", type: "number", applyMode: "hot" },
  { path: "general.modes", section: "general", key: "modes", labelKey: "config.field.modes", type: "string", applyMode: "restart" },
  { path: "censorship.tls_domain", section: "censorship", key: "tls_domain", labelKey: "config.field.tls_domain", type: "string", applyMode: "restart" },
  { path: "censorship.tls_domains", section: "censorship", key: "tls_domains", labelKey: "config.field.tls_domains", type: "string[]", applyMode: "restart" },
  { path: "timeouts.client_handshake", section: "timeouts", key: "client_handshake", labelKey: "config.field.client_handshake", type: "number", applyMode: "restart" },
];

export function fieldsBySection(): Record<string, ConfigField[]> {
  const out: Record<string, ConfigField[]> = {};
  for (const f of CONFIG_FIELDS) (out[f.section] ??= []).push(f);
  return out;
}

// requiresRestart reports whether any changed dotted path maps to a restart field.
export function requiresRestart(changedPaths: string[]): boolean {
  const mode = new Map(CONFIG_FIELDS.map((f) => [f.path, f.applyMode] as const));
  return changedPaths.some((p) => mode.get(p) === "restart");
}
