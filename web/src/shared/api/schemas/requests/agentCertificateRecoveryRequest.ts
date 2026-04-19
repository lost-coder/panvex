import { z } from "zod";

export const agentCertificateRecoveryRequestSchema = z.object({
  agent_id: z.string().min(1).max(128),
  certificate_pem: z.string().min(1),
  proof_timestamp_unix: z.number().int().positive(),
  proof_nonce: z.string().min(1).max(128),
  proof_signature: z.string().min(1),
});

export type AgentCertificateRecoveryRequest = z.infer<
  typeof agentCertificateRecoveryRequestSchema
>;
