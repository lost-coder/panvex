import { z } from "zod";

/**
 * R-Q-20: Zod schemas for the operator settings endpoints
 * (/settings/panel, /settings/retention, /settings/updates,
 * /settings/appearance).
 */

export const panelSettingsSchema = z.object({
  http_public_url: z.string(),
  grpc_public_endpoint: z.string(),
});

export const retentionSettingsSchema = z.object({
  ts_raw_seconds: z.number().int(),
  ts_hourly_seconds: z.number().int(),
  ts_dc_seconds: z.number().int(),
  ip_history_seconds: z.number().int(),
  event_history_seconds: z.number().int(),
});

export const updateSettingsSchema = z.object({
  channel: z.string(),
  auto_check: z.boolean(),
  // Backend may add fields here as the auto-update story evolves; the
  // UI reads only channel + auto_check today.
});

export const appearanceSettingsSchema = z.object({
  theme: z.enum(["system", "light", "dark"]),
  density: z.enum(["comfortable", "compact"]),
  help_mode: z.enum(["off", "basic", "full"]),
});

export type PanelSettingsParsed = z.infer<typeof panelSettingsSchema>;
export type RetentionSettingsParsed = z.infer<typeof retentionSettingsSchema>;
export type UpdateSettingsParsed = z.infer<typeof updateSettingsSchema>;
export type AppearanceSettingsParsed = z.infer<typeof appearanceSettingsSchema>;
