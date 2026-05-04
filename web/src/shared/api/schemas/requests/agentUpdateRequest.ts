import { z } from "zod";

/**
 * Mirrors the inline payload accepted by `handleAgentUpdate` in
 * internal/controlplane/server/http_updates.go (POST
 * /api/agents/{id}/update). Empty `version` means "use latest known"
 * — the server resolves it against its release index. We do not pin
 * a max length: GitHub release tags can be arbitrary, and the server
 * already validates the resolved version itself.
 */
export const agentUpdateRequestSchema = z.object({
  version: z.string().max(128).optional().default(""),
});

export type AgentUpdateRequest = z.infer<typeof agentUpdateRequestSchema>;
