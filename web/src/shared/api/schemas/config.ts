import { z } from "zod";

/**
 * R-Q-20: Zod schemas for the config endpoints:
 * - GET  /api/agents/{id}/config
 * - PUT  /api/agents/{id}/config
 * - POST /api/agents/{id}/config/apply
 * - GET  /api/fleet-groups/{id}/config
 * - PUT  /api/fleet-groups/{id}/config
 * - POST /api/fleet-groups/{id}/config/apply
 *
 * Schemas mirror the runtime types declared in shared/api/config.ts
 * so the api<T>() ZodType<T> overload accepts them.
 */

export const configSectionsSchema = z.record(z.string(), z.unknown());

export const configDriftSchema = z.object({
  status: z.enum(["in_sync", "drifted", "unknown"]),
  fields: z.array(z.string()).default([]),
});

export const agentConfigSchema = z.object({
  override: configSectionsSchema.default({}),
  effective: configSectionsSchema.default({}),
  observed: configSectionsSchema.default({}),
  drift: configDriftSchema,
});

export const groupConfigNodeSchema = z.object({
  agent_id: z.string(),
  status: z.string(),
});

export const groupConfigSchema = z.object({
  sections: configSectionsSchema.default({}),
  nodes: z.array(groupConfigNodeSchema).default([]),
});

export const applyResultSchema = z.object({
  applied: z.number().default(0),
  failed: z.string().default(""),
  error: z.string().default(""),
});

// Async group apply (audit MEDIUM): POST /fleet-groups/{id}/config/apply now
// returns 202 Accepted with a batch id + one job handle per in-scope agent,
// instead of blocking the request until every agent's job is terminal. An
// agent with an empty effective config carries an empty job_id (no-op).
export const groupApplyJobHandleSchema = z.object({
  agent_id: z.string(),
  job_id: z.string().default(""),
});

export const groupApplyAcceptedSchema = z.object({
  batch_id: z.string(),
  jobs: z.array(groupApplyJobHandleSchema).default([]),
});

// The per-agent status returned by GET /fleet-groups/{id}/config/apply/status.
export const groupApplyAgentStatusSchema = z.object({
  agent_id: z.string(),
  job_id: z.string().default(""),
  status: z.enum(["pending", "running", "succeeded", "failed"]),
  message: z.string().default(""),
});

export const groupApplyStatusSchema = z.object({
  done: z.boolean().default(false),
  total: z.number().default(0),
  applied: z.number().default(0),
  failed: z.number().default(0),
  pending: z.number().default(0),
  agents: z.array(groupApplyAgentStatusSchema).default([]),
});

// Request body schema for PUT endpoints.
export const configSectionsRequestSchema = z.object({
  sections: configSectionsSchema,
});

export type AgentConfig = z.infer<typeof agentConfigSchema>;
export type GroupConfig = z.infer<typeof groupConfigSchema>;
export type ApplyResult = z.infer<typeof applyResultSchema>;
export type ConfigSections = z.infer<typeof configSectionsSchema>;
export type GroupConfigNode = z.infer<typeof groupConfigNodeSchema>;
export type GroupApplyAccepted = z.infer<typeof groupApplyAcceptedSchema>;
export type GroupApplyJobHandle = z.infer<typeof groupApplyJobHandleSchema>;
export type GroupApplyStatus = z.infer<typeof groupApplyStatusSchema>;
export type GroupApplyAgentStatus = z.infer<typeof groupApplyAgentStatusSchema>;
