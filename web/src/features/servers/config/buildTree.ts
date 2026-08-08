// P4-T1: pure view-model that merges the generated param catalog with a
// node's desired/observed config into a section -> fields tree, flagging
// drift, group-locked paths, readonly (process-owned/container) fields, and
// observed paths with no catalog entry ("unknown").
//
// The catalog (PARAM_CATALOG) never lists container-shaped paths
// (upstreams, dc_overrides, censorship.exclusive_mask.*) — they're
// structurally editable elsewhere in the config, not through the flat
// param editor. The path LIST lives in paramCatalog.CONTAINER_PATHS; the
// isContainerPath predicate below is defined here (and exported for
// ConfigTree's own filtering) and treats a match as "known but
// uneditable", distinct from "unknown parameter".
//
// Values are flattened with a generic recursive walk rather than
// `flattenSections` (./sections): that helper only emits paths present in
// CONFIG_FIELDS (i.e. the catalog itself), which would silently drop any
// observed path with no catalog entry — exactly the "unknown" case this
// view model needs to surface.
import { CONTAINER_PATHS, PARAM_CATALOG, catalogEntry, type ParamCatalogEntry } from "./paramCatalog";
import { isProcessOwned } from "./guard";
import type { ConfigSections } from "@/shared/api/schemas/config";

export interface TreeField {
  entry: ParamCatalogEntry | null;
  path: string;
  value: unknown;
  observed: unknown;
  drifted: boolean;
  locked: boolean;
  readonly: boolean;
  unknown: boolean;
  /**
   * Есть ли путь хоть в desired, хоть в observed. false означает, что Telemt
   * поле не сериализовал — оно не задано в конфиге ноды и работает по
   * встроенному дефолту. Отличать это от «задано и равно пустому/false»
   * обязательно: иначе панель показывает выключенный тумблер там, где на
   * деле действует дефолт.
   */
  present: boolean;
}

export interface TreeSection {
  section: string;
  fields: TreeField[];
}

/** Exported so ConfigTree can filter container paths (and their children) out of what it renders — see its own comment for why. */
export function isContainerPath(path: string): boolean {
  return CONTAINER_PATHS.some((c) => path === c || path.startsWith(`${c}.`));
}

// Recursively flattens a nested config-sections object into a
// { "section.key" : value } map. Unlike flattenSections (./sections), this
// walks every key present in the object, not just curated catalog paths —
// needed so unknown/unmanaged observed paths still surface in the tree.
function flattenAll(obj: unknown, prefix = ""): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  if (obj === null || typeof obj !== "object") return out;
  for (const [key, value] of Object.entries(obj as Record<string, unknown>)) {
    const path = prefix ? `${prefix}.${key}` : key;
    if (value !== null && typeof value === "object" && !Array.isArray(value)) {
      Object.assign(out, flattenAll(value, path));
    } else {
      out[path] = value;
    }
  }
  return out;
}

function sectionOf(path: string): string {
  return catalogEntry(path)?.section ?? path.split(".")[0] ?? path;
}

export function buildTree(
  desired: ConfigSections,
  observed: ConfigSections,
  groupPaths: Set<string>,
): TreeSection[] {
  const desiredFlat = flattenAll(desired);
  const observedFlat = flattenAll(observed);

  const paths = new Set<string>();
  for (const e of PARAM_CATALOG) paths.add(e.path);
  for (const path of Object.keys(observedFlat)) paths.add(path);

  const fields: TreeField[] = [];
  for (const path of paths) {
    const entry = catalogEntry(path) ?? null;
    const hasDesired = Object.prototype.hasOwnProperty.call(desiredFlat, path);
    const hasObserved = Object.prototype.hasOwnProperty.call(observedFlat, path);
    const observedValue = observedFlat[path];
    const value = hasDesired ? desiredFlat[path] : observedValue;
    const container = isContainerPath(path);

    fields.push({
      entry,
      path,
      value,
      observed: observedValue,
      drifted:
        hasDesired && hasObserved && JSON.stringify(value) !== JSON.stringify(observedValue),
      locked: groupPaths.has(path),
      readonly: !entry || isProcessOwned(path) || container,
      unknown: !entry && !container,
      present: hasDesired || hasObserved,
    });
  }

  const bySection = new Map<string, TreeField[]>();
  for (const field of fields) {
    const section = sectionOf(field.path);
    const list = bySection.get(section);
    if (list) list.push(field);
    else bySection.set(section, [field]);
  }

  const catalogOrder: string[] = [];
  for (const e of PARAM_CATALOG) {
    if (!catalogOrder.includes(e.section)) catalogOrder.push(e.section);
  }

  const sections: TreeSection[] = Array.from(bySection, ([section, sectionFields]) => ({
    section,
    fields: sectionFields,
  }));

  sections.sort((a, b) => {
    const ai = catalogOrder.indexOf(a.section);
    const bi = catalogOrder.indexOf(b.section);
    if (ai !== -1 && bi !== -1) return ai - bi;
    if (ai !== -1) return -1;
    if (bi !== -1) return 1;
    return a.section.localeCompare(b.section);
  });

  return sections;
}
