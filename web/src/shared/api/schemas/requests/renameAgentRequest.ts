import { z } from "zod";

export const renameAgentRequestSchema = z.object({
  node_name: z.string().min(1).max(256),
});

export type RenameAgentRequest = z.infer<typeof renameAgentRequestSchema>;
