import { api, apiBasePath, encodeRequest, type RequestOpts } from "./http";
import {
  agentApplyConfigRequestSchema,
  agentConfigSchema,
  applyAcceptedSchema,
  applyConfigRequestSchema,
  configSectionsRequestSchema,
  groupApplyActiveBatchSchema,
  groupApplyBatchStatusSchema,
  groupConfigSchema,
  type AgentApplyConfigRequest,
  type AgentConfig,
  type ApplyAccepted,
  type ApplyConfigRequest,
  type ConfigSections,
  type GroupApplyActiveBatch,
  type GroupApplyBatchStatus,
  type GroupConfig,
} from "./schemas";

export type {
  AgentApplyConfigRequest,
  AgentConfig,
  ApplyAccepted,
  ApplyConfigRequest,
  ConfigSections,
  GroupApplyActiveBatch,
  GroupApplyBatchStatus,
  GroupConfig,
};

export const configApi = {
  // R-Q-20: Zod parse on every response that carries a body.

  getAgentConfig: (id: string, opts?: RequestOpts) =>
    api<AgentConfig>(`${apiBasePath}/agents/${id}/config`, { signal: opts?.signal }, agentConfigSchema),

  putAgentConfig: (id: string, sections: ConfigSections) =>
    api<void>(`${apiBasePath}/agents/${id}/config`, {
      method: "PUT",
      body: encodeRequest(
        `${apiBasePath}/agents/${id}/config`,
        configSectionsRequestSchema,
        { sections },
      ),
    }),

  // P3-3.4: single apply is a persistent batch-of-one; 202 + batch_id, progress
  // polled via getAgentConfigApplyBatch. `policy` is the optional reload
  // session policy (instant vs. drain); an absent policy sends an empty
  // body, which the panel resolves to its own default (drain/30s).
  applyAgentConfig: (id: string, policy?: AgentApplyConfigRequest) =>
    api<ApplyAccepted>(
      `${apiBasePath}/agents/${id}/config/apply`,
      {
        method: "POST",
        body: encodeRequest(
          `${apiBasePath}/agents/${id}/config/apply`,
          agentApplyConfigRequestSchema,
          policy ?? {},
        ),
      },
      applyAcceptedSchema,
    ),

  // Persistent-batch aggregate of the single apply. Same JSON shape as the
  // group batch status — reuse groupApplyBatchStatusSchema.
  getAgentConfigApplyBatch: (id: string, batchId: string, opts?: RequestOpts) =>
    api<GroupApplyBatchStatus>(
      `${apiBasePath}/agents/${id}/config/apply/batches/${batchId}`,
      { signal: opts?.signal },
      groupApplyBatchStatusSchema,
    ),

  getGroupConfig: (id: string, opts?: RequestOpts) =>
    api<GroupConfig>(`${apiBasePath}/fleet-groups/${id}/config`, { signal: opts?.signal }, groupConfigSchema),

  putGroupConfig: (id: string, sections: ConfigSections) =>
    api<void>(`${apiBasePath}/fleet-groups/${id}/config`, {
      method: "PUT",
      body: encodeRequest(
        `${apiBasePath}/fleet-groups/${id}/config`,
        configSectionsRequestSchema,
        { sections },
      ),
    }),

  // Async group apply: returns 202 with a batch id. Progress is polled via
  // getGroupConfigApplyBatch, built from the persisted batch + target rows.
  // `policy` is the optional reload session policy (instant vs. drain); an
  // absent policy sends an empty body, which the panel resolves to its own
  // default (drain/30s).
  applyGroupConfig: (id: string, policy?: ApplyConfigRequest) =>
    api<ApplyAccepted>(
      `${apiBasePath}/fleet-groups/${id}/config/apply`,
      {
        method: "POST",
        body: encodeRequest(
          `${apiBasePath}/fleet-groups/${id}/config/apply`,
          applyConfigRequestSchema,
          policy ?? {},
        ),
      },
      applyAcceptedSchema,
    ),

  // Persistent-batch aggregate for a single rollout, keyed by the batch id
  // POST .../config/apply returned. Built entirely from the stored batch +
  // target rows, so it can be re-fetched after a browser reload or from a
  // different device — the whole point of Phase A persisting batches.
  getGroupConfigApplyBatch: (id: string, batchId: string, opts?: RequestOpts) =>
    api<GroupApplyBatchStatus>(
      `${apiBasePath}/fleet-groups/${id}/config/apply/batches/${batchId}`,
      { signal: opts?.signal },
      groupApplyBatchStatusSchema,
    ),

  // Resolves the fleet group's currently-running config-apply batch, if
  // any. This is the entry point a dashboard uses to discover whether
  // there is anything to resume-poll on mount, without needing to
  // remember a batch id across a page reload. The backend answers 204 No
  // Content (no body) when nothing is in flight; `api()` resolves that to
  // undefined before the schema is consulted, so the return type reflects
  // that honestly rather than lying about a guaranteed batch_id.
  activeGroupConfigApplyBatch: (id: string, opts?: RequestOpts): Promise<GroupApplyActiveBatch | undefined> =>
    api(
      `${apiBasePath}/fleet-groups/${id}/config/apply/batches?active=1`,
      { signal: opts?.signal },
      groupApplyActiveBatchSchema,
    ),
};
