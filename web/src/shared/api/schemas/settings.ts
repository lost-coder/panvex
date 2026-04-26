import { z } from "zod";

/**
 * R-Q-20: Zod schemas for the operator settings endpoints
 * (/settings/panel, /settings/retention, /settings/updates,
 * /settings/appearance).
 *
 * Schemas mirror the runtime types declared in shared/api/settings.ts
 * exactly so the api<T>() ZodType<T> overload accepts them under
 * exactOptionalPropertyTypes.
 */

export const panelSettingsResponseSchema = z.object({
  http_public_url: z.string(),
  http_root_path: z.string(),
  grpc_public_endpoint: z.string(),
  http_listen_address: z.string(),
  grpc_listen_address: z.string(),
  tls_mode: z.enum(["proxy", "direct"]),
  tls_cert_file: z.string(),
  tls_key_file: z.string(),
  runtime_source: z.enum(["legacy", "config_file"]),
  runtime_config_path: z.string(),
  updated_at_unix: z.number().int(),
  restart: z.object({
    supported: z.boolean(),
    pending: z.boolean(),
    state: z.enum(["ready", "pending", "unavailable"]),
  }),
});

export const appearanceSettingsResponseSchema = z.object({
  theme: z.enum(["system", "light", "dark"]),
  density: z.enum(["comfortable", "compact"]),
  help_mode: z.enum(["off", "basic", "full"]),
  updated_at_unix: z.number().int(),
});

export const retentionSettingsSchema = z.object({
  ts_raw_seconds: z.number().int(),
  ts_hourly_seconds: z.number().int(),
  ts_dc_seconds: z.number().int(),
  ip_history_seconds: z.number().int(),
  event_history_seconds: z.number().int(),
});

export type PanelSettingsResponseParsed = z.infer<typeof panelSettingsResponseSchema>;
export type AppearanceSettingsResponseParsed = z.infer<typeof appearanceSettingsResponseSchema>;
export type RetentionSettingsParsed = z.infer<typeof retentionSettingsSchema>;
