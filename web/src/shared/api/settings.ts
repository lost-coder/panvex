import { api, apiBasePath, encodeRequest } from "./http";
import {
  updateAppearanceSettingsRequestSchema,
  updatePanelSettingsRequestSchema,
  versionSchema,
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
  panelSettings: () => api<PanelSettingsResponse>(`${apiBasePath}/settings/panel`),
  appearanceSettings: () => api<AppearanceSettingsResponse>(`${apiBasePath}/settings/appearance`),
  updateAppearanceSettings: (payload: {
    theme: "system" | "light" | "dark";
    density: "comfortable" | "compact";
    help_mode: "off" | "basic" | "full";
  }) =>
    api<AppearanceSettingsResponse>(`${apiBasePath}/settings/appearance`, {
      method: "PUT",
      body: encodeRequest(
        `${apiBasePath}/settings/appearance`,
        updateAppearanceSettingsRequestSchema,
        payload,
      ),
    }),
  updatePanelSettings: (payload: {
    http_public_url: string;
    grpc_public_endpoint: string;
  }) =>
    api<PanelSettingsResponse>(`${apiBasePath}/settings/panel`, {
      method: "PUT",
      body: encodeRequest(
        `${apiBasePath}/settings/panel`,
        updatePanelSettingsRequestSchema,
        payload,
      ),
    }),
  restartPanel: () =>
    api<PanelSettingsResponse>(`${apiBasePath}/settings/panel/restart`, {
      method: "POST"
    }),
  getRetentionSettings: () => api<RetentionSettings>(`${apiBasePath}/settings/retention`),
  putRetentionSettings: (settings: RetentionSettings) =>
    api<RetentionSettings>(`${apiBasePath}/settings/retention`, {
      method: "PUT",
      body: JSON.stringify(settings),
    }),
  /**
   * GET /api/version — returns different shapes by role (P1-SEC-15). The
   * zod union handles both viewer and operator branches; consumers should
   * narrow via `isOperatorVersion(v)` from lib/schemas/version.
   */
  version: () => api<VersionParsed>(`${apiBasePath}/version`, undefined, versionSchema),
};
