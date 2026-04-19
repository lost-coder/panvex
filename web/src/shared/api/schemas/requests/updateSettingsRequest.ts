import { z } from "zod";

export const agentDownloadSourceSchema = z.enum(["github", "panel"]);
export type AgentDownloadSource = z.infer<typeof agentDownloadSourceSchema>;

export const updateSettingsRequestSchema = z.object({
  check_interval_hours: z.number().int().positive().max(24 * 365).optional(),
  auto_update_panel: z.boolean().optional(),
  auto_update_agents: z.boolean().optional(),
  github_repo: z.string().max(256).optional(),
  github_token: z.string().max(512).optional(),
  agent_download_source: agentDownloadSourceSchema.optional(),
});

export type UpdateSettingsRequest = z.infer<typeof updateSettingsRequestSchema>;
