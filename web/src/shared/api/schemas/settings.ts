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
  password_min_length: z.number().int().min(8).max(128),
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

export const geoipSourceSchema = z.object({
  enabled: z.boolean(),
  url: z.string().optional().default(""),
  local_path: z.string().optional().default(""),
});

export const geoipSettingsSchema = z.object({
  mode: z.enum(["", "auto", "url", "local"]),
  city: geoipSourceSchema,
  asn: geoipSourceSchema,
});

export const geoipSourceStateSchema = z.object({
  last_checked_at: z.number().int().optional().default(0),
  last_updated_at: z.number().int().optional().default(0),
  etag: z.string().optional().default(""),
  path: z.string().optional().default(""),
  size_bytes: z.number().int().optional().default(0),
  error: z.string().optional().default(""),
});

export const geoipStateSchema = z.object({
  city: geoipSourceStateSchema,
  asn: geoipSourceStateSchema,
});

export const geoipResponseSchema = z.object({
  settings: geoipSettingsSchema,
  state: geoipStateSchema,
});

export type PanelSettingsResponseParsed = z.infer<typeof panelSettingsResponseSchema>;
export type AppearanceSettingsResponseParsed = z.infer<typeof appearanceSettingsResponseSchema>;
export type RetentionSettingsParsed = z.infer<typeof retentionSettingsSchema>;
export type GeoIPSettingsParsed = z.infer<typeof geoipSettingsSchema>;
export type GeoIPStateParsed = z.infer<typeof geoipStateSchema>;
export type GeoIPResponseParsed = z.infer<typeof geoipResponseSchema>;

// Registry endpoints — settings schema, values, and restart status.

export const schemaEntrySchema = z.object({
  name: z.string(),
  class: z.enum(["bootstrap", "operational"]),
  type: z.enum(["int", "duration", "string", "bool", "hostport", "url", "enum", "json"]),
  default: z.string().optional(),
  min: z.string().optional(),
  max: z.string().optional(),
  values: z.array(z.string()).optional(),
  env: z.string().optional(),
  toml: z.string().optional(),
  secret: z.boolean().optional(),
  store: z.string().optional(),
  restart: z.boolean().optional(),
  desc: z.string(),
});

export const valuesEntrySchema = z.object({
  value: z.unknown(),
  source: z.enum(["env", "config_file", "db", "default"]),
  source_path: z.string().optional(),
  env_var: z.string().optional(),
  secret: z.boolean().optional(),
  locked: z.boolean(),
  pending_restart: z.boolean().optional(),
  pending_value: z.unknown().optional(),
});

export const valuesResponseSchema = z.object({
  bootstrap: z.record(z.string(), valuesEntrySchema),
  operational: z.record(z.string(), valuesEntrySchema),
});

export const restartStatusSchema = z.object({
  pending: z.boolean(),
  fields: z.array(z.string()),
});

export const schemaArraySchema = z.array(schemaEntrySchema);

export type SchemaEntry = z.infer<typeof schemaEntrySchema>;
export type ValuesEntry = z.infer<typeof valuesEntrySchema>;
export type ValuesResponse = z.infer<typeof valuesResponseSchema>;
export type RestartStatus = z.infer<typeof restartStatusSchema>;
