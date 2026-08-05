// P1 compat shim: CONFIG_FIELDS used to be a hand-curated list of ~29
// editable Telemt config fields. It is now derived from PARAM_CATALOG
// (paramCatalog.ts / Task 6), the generated catalog covering Telemt's full
// PATCH /v1/config surface (215 fields after process-owned paths are
// filtered out). This keeps the existing consumers (ConfigSectionEditor,
// ObservedConfigViewer, sections.ts) compiling against the same ConfigField
// shape without a rewrite; P4 replaces those consumers with a
// catalog-native UI and deletes this file.
//
// labelKey used to be an i18n key (`config.field.<name>`) with hand-written
// translations. There is no such i18n label for the generated catalog, so
// labelKey now carries the raw field key instead; consumers that render
// t(field.labelKey) fall back to i18next's default behavior of returning
// the key itself when no translation exists.
//
// general.data_path, general.quota_state_path and general.disable_colors are
// process-owned: Telemt reads them only at process start, and there is no
// process restart anymore under Maestro reload. PARAM_CATALOG already
// excludes them, so they NEVER appear in CONFIG_FIELDS (the read-only
// viewer keeps them visible but not editable).
import { PARAM_CATALOG, type ParamCatalogEntry } from "./paramCatalog";

export type ApplyMode = "hot" | "reload";
export type FieldType = "string" | "number" | "boolean" | "string[]" | "select";

export interface ConfigField {
  path: string; // "section.key"
  section: string;
  key: string;
  labelKey: string; // no i18n label anymore; consumers show the raw key
  type: FieldType;
  applyMode: ApplyMode;
  options?: string[]; // for select
}

export const CONFIG_FIELDS: ConfigField[] = PARAM_CATALOG.map((e: ParamCatalogEntry) => ({
  path: e.path,
  section: e.section,
  key: e.key,
  labelKey: e.key,
  type: e.type as FieldType,
  applyMode: e.applyMode,
  ...(e.options ? { options: e.options } : {}),
}));

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
