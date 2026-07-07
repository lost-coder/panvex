import { z } from "zod";

import type { components } from "../../openapi.gen.ts";

type Gen = components["schemas"];

export const agentCertificateRecoveryGrantRequestSchema = z.object({
  ttl_seconds: z.number().int().positive().max(60 * 60 * 24 * 7),
}) satisfies z.ZodType<Gen["CreateCertificateRecoveryGrantRequest"]>;

export type AgentCertificateRecoveryGrantRequest = z.infer<
  typeof agentCertificateRecoveryGrantRequestSchema
>;
