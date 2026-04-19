import { z } from "zod";

export const agentBootstrapRequestSchema = z.object({
  node_name: z.string().min(1).max(256),
  version: z.string().min(1).max(64),
});

export type AgentBootstrapRequest = z.infer<typeof agentBootstrapRequestSchema>;
