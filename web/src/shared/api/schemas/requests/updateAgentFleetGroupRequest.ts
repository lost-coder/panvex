import { z } from "zod";

export const updateAgentFleetGroupRequestSchema = z.object({
  fleet_group_id: z.string().min(1).max(64),
});

export type UpdateAgentFleetGroupRequest = z.infer<typeof updateAgentFleetGroupRequestSchema>;
