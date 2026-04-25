import { api, apiBasePath, encodeRequest } from "./http";
import { createJobRequestSchema } from "./schemas";

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
  jobs: () => api<Job[]>(`${apiBasePath}/jobs`),
  audit: () => api<AuditEvent[]>(`${apiBasePath}/audit`),
  createJob: (payload: JobCreateInput) =>
    api<Job>(`${apiBasePath}/jobs`, {
      method: "POST",
      body: encodeRequest(`${apiBasePath}/jobs`, createJobRequestSchema, payload),
    }),
};
