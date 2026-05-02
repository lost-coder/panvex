import { z } from "zod";

/**
 * R-Q-20: Zod schemas for the /settings/updates and related endpoints.
 *
 * Schemas mirror the runtime types declared in shared/api/updates.ts
 * exactly so the api<T>() ZodType<T> overload accepts them.
 */

export const updateSettingsSchema = z.object({
  check_interval_hours: z.number(),
  auto_update_panel: z.boolean(),
  auto_update_agents: z.boolean(),
  github_repo: z.string(),
  github_token: z.string(),
  agent_download_source: z.string(),
});

export const updateStateSchema = z.object({
  latest_panel_version: z.string(),
  latest_agent_version: z.string(),
  panel_changelog: z.string(),
  agent_changelog: z.string(),
  last_checked_at: z.number(),
});

export const updateSettingsResponseSchema = z.object({
  settings: updateSettingsSchema,
  state: updateStateSchema,
  current_version: z.string(),
});

export const checkForUpdatesResponseSchema = z.object({
  status: z.string(),
});

export const updatePanelResponseSchema = z.object({
  status: z.string(),
  from: z.string(),
  to: z.string(),
});

export const updateAgentResponseSchema = z.object({
  job_id: z.string(),
  status: z.string(),
  version: z.string(),
});

export type UpdateSettingsParsed = z.infer<typeof updateSettingsSchema>;
export type UpdateSettingsResponseParsed = z.infer<typeof updateSettingsResponseSchema>;
