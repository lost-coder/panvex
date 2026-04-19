import { z } from "zod";

export const createEnrollmentTokenRequestSchema = z.object({
  fleet_group_id: z.string().min(1),
  ttl_seconds: z.number().int().positive().max(60 * 60 * 24 * 30),
});

export type CreateEnrollmentTokenRequest = z.infer<typeof createEnrollmentTokenRequestSchema>;
