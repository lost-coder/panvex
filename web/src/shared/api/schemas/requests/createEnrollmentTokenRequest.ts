import { z } from "zod";

// Backend treats an empty fleet_group_id as "default group", so we intentionally
// allow the empty string here instead of rejecting with a frontend schema
// error that users can't act on from the UI.
export const createEnrollmentTokenRequestSchema = z.object({
  fleet_group_id: z.string().max(128),
  ttl_seconds: z.number().int().positive().max(60 * 60 * 24 * 30),
});

export type CreateEnrollmentTokenRequest = z.infer<typeof createEnrollmentTokenRequestSchema>;
