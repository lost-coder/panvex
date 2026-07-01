import { z } from "zod";

/**
 * Mirrors `geoip.Settings` (internal/controlplane/geoip/state.go), the
 * exact body decoded by handlePutGeoIPSettings
 * (internal/controlplane/server/http_geoip.go) via decodeJSON. Reuses the
 * same shape as the response's `settings` field
 * (see geoipSettingsSchema in ../settings.ts) — kept as a separate
 * declaration here so request/response schemas stay independently
 * evolvable per the existing requests/ vs settings.ts split.
 */
const geoipSourceRequestSchema = z.object({
  enabled: z.boolean(),
  url: z.string().optional().default(""),
  local_path: z.string().optional().default(""),
});

export const geoipSettingsRequestSchema = z.object({
  mode: z.enum(["", "auto", "url", "local"]),
  city: geoipSourceRequestSchema,
  asn: geoipSourceRequestSchema,
});

export type GeoIPSettingsRequest = z.infer<typeof geoipSettingsRequestSchema>;
