import type { AppearanceSettingsResponse } from "./api";

export type AppearanceTheme = "system" | "light" | "dark";
export type AppearanceDensity = "comfortable" | "compact";
export type AppearanceHelpMode = "off" | "basic" | "full";
export type EffectiveAppearance = {
  theme: "light" | "dark";
  density: AppearanceDensity;
};
export type AppearanceDraft = {
  theme: AppearanceTheme;
  density: AppearanceDensity;
  helpMode: AppearanceHelpMode;
};

export const defaultAppearanceSettings: AppearanceSettingsResponse = {
  theme: "system",
  density: "comfortable",
  help_mode: "basic",
  updated_at_unix: 0
};

export function getAppearanceQueryKey(userID: string | undefined) {
  return ["appearance-settings", userID ?? "anonymous"] as const;
}

export function normalizeAppearanceSettings(
  settings: Partial<AppearanceSettingsResponse> | undefined
): AppearanceSettingsResponse {
  return {
    theme: normalizeAppearanceTheme(settings?.theme),
    density: normalizeAppearanceDensity(settings?.density),
    help_mode: normalizeAppearanceHelpMode(settings?.help_mode),
    updated_at_unix: typeof settings?.updated_at_unix === "number" ? settings.updated_at_unix : 0
  };
}

export function buildAppearanceDraft(
  settings: Partial<AppearanceSettingsResponse> | undefined
): AppearanceDraft {
  const normalized = normalizeAppearanceSettings(settings);
  return {
    theme: normalized.theme,
    density: normalized.density,
    helpMode: normalized.help_mode
  };
}

export function syncAppearanceDraft(
  currentDraft: AppearanceDraft,
  settings: Partial<AppearanceSettingsResponse> | undefined,
  isDirty: boolean
): AppearanceDraft {
  if (isDirty) {
    return currentDraft;
  }

  return buildAppearanceDraft(settings);
}

export function resolveEffectiveAppearance(
  settings: AppearanceSettingsResponse,
  prefersDark: boolean
): EffectiveAppearance {
  const normalized = normalizeAppearanceSettings(settings);
  return {
    theme: normalized.theme === "system" ? (prefersDark ? "dark" : "light") : normalized.theme,
    density: normalized.density
  };
}

export function normalizeAppearanceTheme(value: string | undefined): AppearanceTheme {
  if (value === "light" || value === "dark") {
    return value;
  }

  return "system";
}

export function normalizeAppearanceDensity(value: string | undefined): AppearanceDensity {
  if (value === "compact") {
    return value;
  }

  return "comfortable";
}

export function normalizeAppearanceHelpMode(value: string | undefined): AppearanceHelpMode {
  if (value === "off" || value === "full") {
    return value;
  }

  return "basic";
}

export function applyAppearanceAttributes(
  target: { dataset: Record<string, string | undefined> },
  appearance: EffectiveAppearance
) {
  target.dataset.theme = appearance.theme;
  target.dataset.density = appearance.density;
}

export function clearAppearanceAttributes(target: { dataset: Record<string, string | undefined> }) {
  delete target.dataset.theme;
  delete target.dataset.density;
}
