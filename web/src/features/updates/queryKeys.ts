// BP-02: feature-local React-Query key factory for the updates
// surface (panel + agent self-update settings + state). The hook
// `useUpdates` itself lives in shared/hooks/ for now because
// UpdatesSettingsSection imports it from the settings feature; the
// keys are owned here so future relocation of the hook is a
// no-op for cache identity. Shape preserved verbatim from the
// pre-migration code (`["updates"]`).

export const updatesKeys = {
  /** Root prefix — invalidate to flush the updates settings/state query. */
  all: ["updates"] as const,
  /** Unfiltered settings/state read — same shape as `all`. */
  settings: () => [...updatesKeys.all] as const,
};
