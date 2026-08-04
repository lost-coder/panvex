// Curated editable Telemt config fields for the panel.
//
// applyMode is a UI HINT, sourced from ../../../../../docs/telemt-config-param-catalog.md
// (hot = Telemt's file watcher applies the field live with no reload at all;
// reload = the field needs a config reload). It is NOT a source of truth for
// whether a reload actually happens: Telemt's Maestro in-process reload
// replaced the old "RESTART" path entirely, and at apply time Telemt itself
// reports `runtime_reload_required` on the PATCH response — that field
// decides. applyMode here is only used to decide whether the UI proactively
// asks the operator the session-policy question before applying; if it is
// wrong for some field, the apply itself still behaves correctly, it just
// means the UI over- or under-asks. That's a UX nit, not a correctness bug.
//
// The set is intentionally curated, not exhaustive — extend CONFIG_FIELDS as
// more knobs are surfaced; the read-only config viewer covers everything
// else. Editable sections only: general, timeouts, censorship, upstreams,
// dc_overrides (Telemt's PATCH /v1/config allowlist; show_link is legacy and
// rejected). `upstreams` (an array of entries) and `dc_overrides` (a map
// keyed by DC id) don't fit the flat "section.key" scalar model this
// registry uses, so neither has entries yet.
//
// general.data_path, general.quota_state_path and general.disable_colors are
// process-owned: Telemt reads them only at process start, and there is no
// process restart anymore under Maestro reload. They must NEVER appear in
// CONFIG_FIELDS (the read-only viewer keeps them visible but not editable).
export type ApplyMode = "hot" | "reload";
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
  // -- general --------------------------------------------------------
  { path: "general.log_level", section: "general", key: "log_level", labelKey: "config.field.log_level", type: "select", applyMode: "hot", options: ["error", "warn", "info", "debug", "trace"] },
  { path: "general.update_every", section: "general", key: "update_every", labelKey: "config.field.update_every", type: "number", applyMode: "hot" },
  { path: "general.ad_tag", section: "general", key: "ad_tag", labelKey: "config.field.ad_tag", type: "string", applyMode: "hot" },
  { path: "general.desync_all_full", section: "general", key: "desync_all_full", labelKey: "config.field.desync_all_full", type: "boolean", applyMode: "hot" },
  { path: "general.hardswap", section: "general", key: "hardswap", labelKey: "config.field.hardswap", type: "boolean", applyMode: "hot" },
  { path: "general.me_reinit_every_secs", section: "general", key: "me_reinit_every_secs", labelKey: "config.field.me_reinit_every_secs", type: "number", applyMode: "hot" },
  { path: "general.modes", section: "general", key: "modes", labelKey: "config.field.modes", type: "string", applyMode: "reload" },
  { path: "general.prefer_ipv6", section: "general", key: "prefer_ipv6", labelKey: "config.field.prefer_ipv6", type: "boolean", applyMode: "reload" },
  { path: "general.fast_mode", section: "general", key: "fast_mode", labelKey: "config.field.fast_mode", type: "boolean", applyMode: "reload" },
  { path: "general.use_middle_proxy", section: "general", key: "use_middle_proxy", labelKey: "config.field.use_middle_proxy", type: "boolean", applyMode: "reload" },
  { path: "general.ntp_check", section: "general", key: "ntp_check", labelKey: "config.field.ntp_check", type: "boolean", applyMode: "reload" },
  { path: "general.auto_degradation_enabled", section: "general", key: "auto_degradation_enabled", labelKey: "config.field.auto_degradation_enabled", type: "boolean", applyMode: "reload" },
  { path: "general.rst_on_close", section: "general", key: "rst_on_close", labelKey: "config.field.rst_on_close", type: "select", applyMode: "reload", options: ["off", "errors", "always"] },

  // -- timeouts (all RESTART in the catalog -> reload under Maestro) --
  { path: "timeouts.client_first_byte_idle_secs", section: "timeouts", key: "client_first_byte_idle_secs", labelKey: "config.field.client_first_byte_idle_secs", type: "number", applyMode: "reload" },
  { path: "timeouts.client_handshake", section: "timeouts", key: "client_handshake", labelKey: "config.field.client_handshake", type: "number", applyMode: "reload" },
  { path: "timeouts.relay_client_idle_soft_secs", section: "timeouts", key: "relay_client_idle_soft_secs", labelKey: "config.field.relay_client_idle_soft_secs", type: "number", applyMode: "reload" },
  { path: "timeouts.relay_client_idle_hard_secs", section: "timeouts", key: "relay_client_idle_hard_secs", labelKey: "config.field.relay_client_idle_hard_secs", type: "number", applyMode: "reload" },
  { path: "timeouts.client_keepalive", section: "timeouts", key: "client_keepalive", labelKey: "config.field.client_keepalive", type: "number", applyMode: "reload" },
  { path: "timeouts.client_ack", section: "timeouts", key: "client_ack", labelKey: "config.field.client_ack", type: "number", applyMode: "reload" },

  // -- censorship (all RESTART in the catalog -> reload under Maestro) --
  { path: "censorship.tls_domain", section: "censorship", key: "tls_domain", labelKey: "config.field.tls_domain", type: "string", applyMode: "reload" },
  { path: "censorship.tls_domains", section: "censorship", key: "tls_domains", labelKey: "config.field.tls_domains", type: "string[]", applyMode: "reload" },
  { path: "censorship.unknown_sni_action", section: "censorship", key: "unknown_sni_action", labelKey: "config.field.unknown_sni_action", type: "select", applyMode: "reload", options: ["drop", "mask", "accept", "reject_handshake"] },
  { path: "censorship.mask", section: "censorship", key: "mask", labelKey: "config.field.mask", type: "boolean", applyMode: "reload" },
  { path: "censorship.mask_host", section: "censorship", key: "mask_host", labelKey: "config.field.mask_host", type: "string", applyMode: "reload" },
  { path: "censorship.mask_port", section: "censorship", key: "mask_port", labelKey: "config.field.mask_port", type: "number", applyMode: "reload" },
  { path: "censorship.tls_emulation", section: "censorship", key: "tls_emulation", labelKey: "config.field.tls_emulation", type: "boolean", applyMode: "reload" },
  { path: "censorship.server_hello_delay_min_ms", section: "censorship", key: "server_hello_delay_min_ms", labelKey: "config.field.server_hello_delay_min_ms", type: "number", applyMode: "reload" },
  { path: "censorship.server_hello_delay_max_ms", section: "censorship", key: "server_hello_delay_max_ms", labelKey: "config.field.server_hello_delay_max_ms", type: "number", applyMode: "reload" },
  { path: "censorship.alpn_enforce", section: "censorship", key: "alpn_enforce", labelKey: "config.field.alpn_enforce", type: "boolean", applyMode: "reload" },
];

export function fieldsBySection(): Record<string, ConfigField[]> {
  const out: Record<string, ConfigField[]> = {};
  for (const f of CONFIG_FIELDS) (out[f.section] ??= []).push(f);
  return out;
}

// requiresReload reports whether any changed dotted path maps to a reload field.
export function requiresReload(changedPaths: string[]): boolean {
  const mode = new Map(CONFIG_FIELDS.map((f) => [f.path, f.applyMode] as const));
  return changedPaths.some((p) => mode.get(p) === "reload");
}
