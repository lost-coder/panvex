import { api, apiBasePath, encodeRequest } from "./http";
import {
  appearanceSettingsResponseSchema,
  geoipResponseSchema,
  panelSettingsResponseSchema,
  retentionSettingsSchema,
  restartStatusSchema,
  schemaArraySchema,
  updateAppearanceSettingsRequestSchema,
  updatePanelSettingsRequestSchema,
  valuesResponseSchema,
  versionSchema,
  type GeoIPResponseParsed,
  type GeoIPSettingsParsed,
  type RestartStatus,
  type SchemaEntry,
  type ValuesResponse,
  type VersionParsed,
} from "./schemas";

export type PanelSettingsResponse = {
  http_public_url: string;
  http_root_path: string;
  grpc_public_endpoint: string;
  http_listen_address: string;
  grpc_listen_address: string;
  tls_mode: "proxy" | "direct";
  tls_cert_file: string;
  tls_key_file: string;
  runtime_source: "legacy" | "config_file";
  runtime_config_path: string;
  password_min_length: number;
  updated_at_unix: number;
  restart: {
    supported: boolean;
    pending: boolean;
    state: "ready" | "pending" | "unavailable";
  };
};

export type AppearanceSettingsResponse = {
  theme: "system" | "light" | "dark";
  density: "comfortable" | "compact";
  help_mode: "off" | "basic" | "full";
  updated_at_unix: number;
};

export type RetentionSettings = {
  ts_raw_seconds: number;
  ts_hourly_seconds: number;
  ts_dc_seconds: number;
  ip_history_seconds: number;
  event_history_seconds: number;
};

export const settingsApi = {
  // R-Q-20: Zod parse on every read; the response schemas mirror the
  // runtime types so the api<T>() overload accepts them.
  panelSettings: () =>
    api<PanelSettingsResponse>(
      `${apiBasePath}/settings/panel`,
      undefined,
      panelSettingsResponseSchema,
    ),
  appearanceSettings: () =>
    api<AppearanceSettingsResponse>(
      `${apiBasePath}/settings/appearance`,
      undefined,
      appearanceSettingsResponseSchema,
    ),
  updateAppearanceSettings: (payload: {
    theme: "system" | "light" | "dark";
    density: "comfortable" | "compact";
    help_mode: "off" | "basic" | "full";
  }) =>
    api<AppearanceSettingsResponse>(
      `${apiBasePath}/settings/appearance`,
      {
        method: "PUT",
        body: encodeRequest(
          `${apiBasePath}/settings/appearance`,
          updateAppearanceSettingsRequestSchema,
          payload,
        ),
      },
      appearanceSettingsResponseSchema,
    ),
  updatePanelSettings: (payload: {
    http_public_url: string;
    grpc_public_endpoint: string;
    password_min_length: number;
  }) =>
    api<PanelSettingsResponse>(
      `${apiBasePath}/settings/panel`,
      {
        method: "PUT",
        body: encodeRequest(
          `${apiBasePath}/settings/panel`,
          updatePanelSettingsRequestSchema,
          payload,
        ),
      },
      panelSettingsResponseSchema,
    ),
  restartPanel: () =>
    api<PanelSettingsResponse>(
      `${apiBasePath}/settings/panel/restart`,
      { method: "POST" },
      panelSettingsResponseSchema,
    ),
  getRetentionSettings: () =>
    api<RetentionSettings>(
      `${apiBasePath}/settings/retention`,
      undefined,
      retentionSettingsSchema,
    ),
  putRetentionSettings: (settings: RetentionSettings) =>
    api<RetentionSettings>(
      `${apiBasePath}/settings/retention`,
      {
        method: "PUT",
        body: JSON.stringify(settings),
      },
      retentionSettingsSchema,
    ),
  getGeoIPSettings: () =>
    api<GeoIPResponseParsed>(
      `${apiBasePath}/settings/geoip`,
      undefined,
      geoipResponseSchema,
    ),
  putGeoIPSettings: (settings: GeoIPSettingsParsed) =>
    api<GeoIPResponseParsed>(
      `${apiBasePath}/settings/geoip`,
      {
        method: "PUT",
        body: JSON.stringify(settings),
      },
      geoipResponseSchema,
    ),
  refreshGeoIP: () =>
    api<GeoIPResponseParsed>(
      `${apiBasePath}/settings/geoip/refresh`,
      { method: "POST" },
      geoipResponseSchema,
    ),
  /**
   * GET /api/version — returns different shapes by role (P1-SEC-15). The
   * zod union handles both viewer and operator branches; consumers should
   * narrow via `isOperatorVersion(v)` from lib/schemas/version.
   */
  version: () => api<VersionParsed>(`${apiBasePath}/version`, undefined, versionSchema),
  getSettingsSchema: () =>
    api<SchemaEntry[]>(
      `${apiBasePath}/settings/schema`,
      undefined,
      schemaArraySchema,
    ),
  getSettingsValues: () =>
    api<ValuesResponse>(
      `${apiBasePath}/settings/values`,
      undefined,
      valuesResponseSchema,
    ),
  getRestartStatus: () =>
    api<RestartStatus>(
      `${apiBasePath}/settings/restart-status`,
      undefined,
      restartStatusSchema,
    ),
  putSettingsValues: (updates: Record<string, string | number | boolean>) =>
    api<void>(
      `${apiBasePath}/settings/values`,
      {
        method: "PUT",
        body: JSON.stringify(updates),
      },
    ),
};
