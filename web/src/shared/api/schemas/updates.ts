import { z } from "zod";

import type { components } from "../openapi.gen.ts";
import type { LoosenOptional } from "./common.ts";

/**
 * R-Q-20: Zod schemas for the /settings/updates and related endpoints.
 *
 * Schemas mirror the runtime types declared in shared/api/updates.ts
 * exactly so the api<T>() ZodType<T> overload accepts them.
 */

// selfUpdateStateSchema (R11b Task 3) is the one schema here with an OpenAPI
// counterpart, so it carries the P8.3 drift-guard binding: `satisfies
// z.ZodType<Gen["SelfUpdateState"]>` compares against the optional-loosened
// generated type (see agent.ts / LoosenOptional), and openapi-drift-guard.ts
// asserts the reverse direction (every Zod key exists in the spec).
type Gen = {
  [K in keyof components["schemas"]]: LoosenOptional<components["schemas"][K]>;
};

export const updateSettingsSchema = z.object({
  check_interval_hours: z.number(),
  auto_update_panel: z.boolean(),
  auto_update_agents: z.boolean(),
  github_repo: z.string(),
  github_token: z.string(),
  agent_download_source: z.string(),
  // Telemt version-selection (Task 2): depth of the recent-versions list
  // shown on the node detail page. Always present — LoadSettings normalizes
  // any pre-existing persisted value to 5, and the server clamps writes to
  // 1..20.
  telemt_versions_to_show: z.number(),
});

export const updateStateSchema = z.object({
  latest_panel_version: z.string(),
  latest_agent_version: z.string(),
  panel_changelog: z.string(),
  agent_changelog: z.string(),
  last_checked_at: z.number(),
  // Reason the most recent update check failed (e.g. a GitHub rate-limit
  // message). Omitted by the server after a successful check; defaults to ""
  // so the parsed type stays a plain string.
  last_check_error: z.string().optional().default(""),
  // Telemt release check (Task 10, Telemt Update v1): newest stable release
  // tag of the fixed upstream telemt/telemt repo, its asset base URL, and the
  // independent error for that check. Always present (server sends "" rather
  // than omitting, unlike last_check_error above).
  telemt_latest_version: z.string(),
  telemt_release_base_url: z.string(),
  telemt_last_check_error: z.string(),
  // Telemt version-selection (Task 1): top-N stable release tags, newest
  // first. Always an array — LoadState/Apply normalization guarantees
  // non-nil, so this is required rather than optional/defaulted.
  telemt_available_versions: z.array(z.string()),
});

// Server-persisted panel self-update lifecycle. Defensive defaults so a phase
// with only `phase` populated (the idle / just-started case) still parses.
export const selfUpdateStateSchema = z.object({
  phase: z.enum([
    "",
    "downloading",
    "installing",
    "restart_pending",
    "completed",
    "failed",
  ]),
  from_version: z.string().optional().default(""),
  to_version: z.string().optional().default(""),
  message: z.string().optional().default(""),
  updated_at: z.number().optional().default(0),
}) satisfies z.ZodType<Gen["SelfUpdateState"]>;

export const updateSettingsResponseSchema = z.object({
  settings: updateSettingsSchema,
  state: updateStateSchema,
  current_version: z.string(),
  self_update: selfUpdateStateSchema,
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
export type SelfUpdateStateParsed = z.infer<typeof selfUpdateStateSchema>;
