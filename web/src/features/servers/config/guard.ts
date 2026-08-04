// Task 9: process-owned field guard.
//
// general.data_path, general.quota_state_path and general.disable_colors are
// read by Telemt only at process start — Maestro's in-process reload cannot
// apply changes to them, only a real process restart can (see fieldRegistry.ts
// for the full rationale). CONFIG_FIELDS never lists them, so the editor
// (ConfigSectionEditor) can't render an input for them today. This module is
// the defensive backstop: it lets both the editor's onChange path and the
// read-only viewer check a path against the same source of truth, so a future
// registry mistake (accidentally adding one of these to CONFIG_FIELDS) can't
// make them silently editable.
export const PROCESS_OWNED_PATHS = [
  "general.data_path",
  "general.quota_state_path",
  "general.disable_colors",
] as const;

const PROCESS_OWNED_SET: ReadonlySet<string> = new Set(PROCESS_OWNED_PATHS);

export function isProcessOwned(path: string): boolean {
  return PROCESS_OWNED_SET.has(path);
}
