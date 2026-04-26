import { api, apiBasePath, encodeRequest } from "./http";
import {
  auditEventListSchema,
  createJobRequestSchema,
  jobListSchema,
  jobSchema,
} from "./schemas";

export type JobTarget = {
  agent_id: string;
  status: string;
  result_text: string;
  result_json: string;
  updated_at: string;
};

export type Job = {
  id: string;
  action: string;
  target_agent_ids: string[];
  targets: JobTarget[];
  /** TTL in nanoseconds (Go time.Duration). Divide by 1e9 for seconds. */
  ttl: number;
  idempotency_key: string;
  actor_id: string;
  status: string;
  payload_json: string;
  created_at: string;
};

export type AuditEvent = {
  id: string;
  actor_id: string;
  action: string;
  target_id: string;
  created_at: string;
  details: Record<string, unknown>;
};

export type JobCreateInput = {
  action: string;
  target_agent_ids: string[];
  idempotency_key: string;
  ttl_seconds: number;
};

export const jobsApi = {
  // R-Q-20: response Zod-parse so a backend drift surfaces as
  // ApiSchemaError instead of `undefined` propagation in the UI.
  jobs: () => api<Job[]>(`${apiBasePath}/jobs`, undefined, jobListSchema),
  audit: () => api<AuditEvent[]>(`${apiBasePath}/audit`, undefined, auditEventListSchema),
  createJob: (payload: JobCreateInput) =>
    api<Job>(
      `${apiBasePath}/jobs`,
      {
        method: "POST",
        body: encodeRequest(`${apiBasePath}/jobs`, createJobRequestSchema, payload),
      },
      jobSchema,
    ),
};
