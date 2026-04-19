import { z } from "zod";

export const appearanceThemeSchema = z.enum(["system", "light", "dark"]);
export const appearanceDensitySchema = z.enum(["comfortable", "compact"]);
export const appearanceHelpModeSchema = z.enum(["off", "basic", "full"]);

export type AppearanceTheme = z.infer<typeof appearanceThemeSchema>;
export type AppearanceDensity = z.infer<typeof appearanceDensitySchema>;
export type AppearanceHelpMode = z.infer<typeof appearanceHelpModeSchema>;

export const updateAppearanceSettingsRequestSchema = z.object({
  theme: appearanceThemeSchema,
  density: appearanceDensitySchema,
  help_mode: appearanceHelpModeSchema,
});

export type UpdateAppearanceSettingsRequest = z.infer<
  typeof updateAppearanceSettingsRequestSchema
>;
