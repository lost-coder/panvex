// Phase 4d: shared plain-TS helpers (no React, no side effects).
// Consumers typically import by sub-path; this barrel covers the
// most reached-for names for convenience.
export {
  normalizeRootPath,
  resolveConfiguredRootPath,
  resolveAPIBasePath,
  buildEventsURL,
  getRouterBasepath,
} from "./runtime-path";
export {
  defaultAppearanceSettings,
  getAppearanceQueryKey,
  normalizeAppearanceSettings,
  buildAppearanceDraft,
  syncAppearanceDraft,
  resolveEffectiveAppearance,
  normalizeAppearanceTheme,
  normalizeAppearanceDensity,
  normalizeAppearanceHelpMode,
  applyAppearanceAttributes,
  clearAppearanceAttributes,
} from "./appearance";
export type {
  AppearanceTheme,
  AppearanceDensity,
  AppearanceHelpMode,
  AppearanceDraft,
  EffectiveAppearance,
} from "./appearance";
