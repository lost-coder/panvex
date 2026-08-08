// Typed loader for the generated Telemt config-parameter catalog
// (paramCatalog.gen.json). Filters out process-owned paths — they are read
// by Telemt only at process start and can never be edited through Maestro's
// in-process reload — and exposes typed, indexed accessors.
import catalog from "./paramCatalog.gen.json";

export interface ParamCatalogEntry {
  path: string;
  section: string;
  type: "boolean" | "number" | "string" | "string[]" | "select";
  applyMode: "hot" | "reload";
  options?: string[];
  default?: string;
  en: string;
  ru: string;
}

// general.data_path, general.quota_state_path and general.disable_colors are
// read by Telemt only at process start — Maestro's in-process reload cannot
// apply changes to them, only a real process restart can (see fieldRegistry.ts
// for the full rationale). Moved here (from guard.ts) so the generated
// catalog can be filtered at the source; guard.ts re-exports this constant.
export const PROCESS_OWNED_PATHS = [
  "general.data_path",
  "general.quota_state_path",
  "general.disable_colors",
] as const;

const OWNED = new Set<string>(PROCESS_OWNED_PATHS);

export const CATALOG_VERSION: string = catalog.version;

export const PARAM_CATALOG: ParamCatalogEntry[] = Object.freeze(
  (catalog.fields as ParamCatalogEntry[]).filter((f) => !OWNED.has(f.path)),
) as ParamCatalogEntry[];

const BY_PATH = new Map(PARAM_CATALOG.map((f) => [f.path, f]));

export function catalogEntry(path: string): ParamCatalogEntry | undefined {
  return BY_PATH.get(path);
}

// Контейнеры: узлы, чьи дочерние ключи задаёт оператор, а не схема Telemt.
// Ключи dc_overrides ("203") и censorship.exclusive_mask ("hv24s.metrion.icu")
// сами содержат точки, поэтому адресация плоским dotted-путём для них
// неоднозначна — они редактируются структурно (MapEditor/UpstreamsEditor).
export const CONTAINER_PATHS = [
  "upstreams",
  "dc_overrides",
  "censorship.exclusive_mask",
] as const;

/** Схема ОДНОГО элемента массива upstreams; path здесь относительные ("type", "weight"). */
export const UPSTREAM_FIELDS: ParamCatalogEntry[] = Object.freeze(
  (catalog.upstreamFields ?? []) as ParamCatalogEntry[],
) as ParamCatalogEntry[];
