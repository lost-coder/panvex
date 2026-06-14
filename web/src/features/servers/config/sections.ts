// P5-T5: flatten/unflatten helpers bridging the nested config-sections
// shape (what the API exposes as ConfigSections) and the flat
// dotted-path → value map the editor works in.
//
// Both directions are scoped strictly to the curated CONFIG_FIELDS set:
// flatten ignores any unmanaged keys the agent reports, and unflatten
// only ever writes back curated paths. This keeps the panel from
// round-tripping (and accidentally clobbering) config the UI doesn't
// surface.

import { CONFIG_FIELDS } from "./fieldRegistry";

/**
 * flattenSections turns a nested config sections object into a
 * { "section.key": value } map for ONLY the curated CONFIG_FIELDS paths
 * (unmanaged fields are dropped).
 */
export function flattenSections(sections: Record<string, unknown>): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const f of CONFIG_FIELDS) {
    const sec = sections[f.section];
    if (sec && typeof sec === "object" && f.key in (sec as Record<string, unknown>)) {
      out[f.path] = (sec as Record<string, unknown>)[f.key];
    }
  }
  return out;
}

/**
 * unflattenPaths turns a { "section.key": value } map back into a nested
 * sections object containing ONLY the provided curated paths. Used as the
 * PUT body. Empty values (undefined / "") are omitted so we don't write
 * blank overrides.
 */
export function unflattenPaths(values: Record<string, unknown>): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  const byPath = new Map(CONFIG_FIELDS.map((f) => [f.path, f] as const));
  for (const [path, value] of Object.entries(values)) {
    const f = byPath.get(path);
    if (!f) continue;
    if (value === undefined || value === "") continue; // omit empties
    ((out[f.section] ??= {}) as Record<string, unknown>)[f.key] = value;
  }
  return out;
}
