import { z } from "zod";

export const jobActionSchema = z.enum([
  "runtime.reload",
  "users.create",
  "client.create",
  "client.update",
  "client.delete",
  "client.rotate_secret",
  "telemetry.refresh_diagnostics",
  "agent.self-update",
]);

export type JobAction = z.infer<typeof jobActionSchema>;

export const createJobRequestSchema = z.object({
  action: jobActionSchema,
  target_agent_ids: z.array(z.string().min(1)).min(1),
  idempotency_key: z.string().min(1).max(128),
  ttl_seconds: z.number().int().positive().max(60 * 60 * 24 * 7),
});

export type CreateJobRequest = z.infer<typeof createJobRequestSchema>;
