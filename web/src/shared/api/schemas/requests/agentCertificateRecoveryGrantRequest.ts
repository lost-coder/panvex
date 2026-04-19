import { z } from "zod";

export const agentCertificateRecoveryGrantRequestSchema = z.object({
  ttl_seconds: z.number().int().positive().max(60 * 60 * 24 * 7),
});

export type AgentCertificateRecoveryGrantRequest = z.infer<
  typeof agentCertificateRecoveryGrantRequestSchema
>;
