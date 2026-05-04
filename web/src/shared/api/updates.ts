import { api, apiBasePath, encodeRequest } from "./http";
import {
  agentUpdateRequestSchema,
  checkForUpdatesResponseSchema,
  updateAgentResponseSchema,
  updatePanelResponseSchema,
  updateSettingsResponseSchema,
  updateSettingsSchema,
} from "./schemas";

export interface UpdateSettings {
  check_interval_hours: number;
  auto_update_panel: boolean;
  auto_update_agents: boolean;
  github_repo: string;
  github_token: string;
  agent_download_source: string;
}

export interface UpdateState {
  latest_panel_version: string;
  latest_agent_version: string;
  panel_changelog: string;
  agent_changelog: string;
  last_checked_at: number;
}

export interface UpdateSettingsResponse {
  settings: UpdateSettings;
  state: UpdateState;
  current_version: string;
}

export const updatesApi = {
  // R-Q-20: Zod parse on every read/write; response schemas mirror the
  // runtime types so the api<T>() overload accepts them.
  getUpdateSettings: () =>
    api<UpdateSettingsResponse>(
      `${apiBasePath}/settings/updates`,
      undefined,
      updateSettingsResponseSchema,
    ),
  putUpdateSettings: (settings: UpdateSettings) =>
    api<UpdateSettings>(
      `${apiBasePath}/settings/updates`,
      {
        method: "PUT",
        body: JSON.stringify(settings),
      },
      updateSettingsSchema,
    ),
  checkForUpdates: () =>
    api<{ status: string }>(
      `${apiBasePath}/settings/updates/check`,
      { method: "POST" },
      checkForUpdatesResponseSchema,
    ),
  updatePanel: (version?: string) =>
    api<{ status: string; from: string; to: string }>(
      `${apiBasePath}/settings/panel/update`,
      {
        method: "POST",
        body: JSON.stringify({ version: version || "" }),
      },
      updatePanelResponseSchema,
    ),
  updateAgent: (agentId: string, version?: string) =>
    api<{ job_id: string; status: string; version: string }>(
      `${apiBasePath}/agents/${agentId}/update`,
      {
        method: "POST",
        body: encodeRequest(
          `${apiBasePath}/agents/${agentId}/update`,
          agentUpdateRequestSchema,
          { version: version || "" },
        ),
      },
      updateAgentResponseSchema,
    ),
};
