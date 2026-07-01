import { api, apiBasePath, encodeRequest } from "./http";
import {
  agentConfigSchema,
  applyResultSchema,
  configSectionsRequestSchema,
  groupApplyAcceptedSchema,
  groupApplyActiveBatchSchema,
  groupApplyBatchStatusSchema,
  groupApplyStatusSchema,
  groupConfigSchema,
  type AgentConfig,
  type ApplyResult,
  type ConfigSections,
  type GroupApplyAccepted,
  type GroupApplyActiveBatch,
  type GroupApplyBatchStatus,
  type GroupApplyJobHandle,
  type GroupApplyStatus,
  type GroupConfig,
} from "./schemas";

export type {
  AgentConfig,
  ApplyResult,
  ConfigSections,
  GroupApplyAccepted,
  GroupApplyActiveBatch,
  GroupApplyBatchStatus,
  GroupApplyJobHandle,
  GroupApplyStatus,
  GroupConfig,
};

export const configApi = {
  // R-Q-20: Zod parse on every response that carries a body.

  getAgentConfig: (id: string) =>
    api<AgentConfig>(`${apiBasePath}/agents/${id}/config`, undefined, agentConfigSchema),

  putAgentConfig: (id: string, sections: ConfigSections) =>
    api<void>(`${apiBasePath}/agents/${id}/config`, {
      method: "PUT",
      body: encodeRequest(
        `${apiBasePath}/agents/${id}/config`,
        configSectionsRequestSchema,
        { sections },
      ),
    }),

  applyAgentConfig: (id: string) =>
    api<ApplyResult>(
      `${apiBasePath}/agents/${id}/config/apply`,
      { method: "POST" },
      applyResultSchema,
    ),

  getGroupConfig: (id: string) =>
    api<GroupConfig>(`${apiBasePath}/fleet-groups/${id}/config`, undefined, groupConfigSchema),

  putGroupConfig: (id: string, sections: ConfigSections) =>
    api<void>(`${apiBasePath}/fleet-groups/${id}/config`, {
      method: "PUT",
      body: encodeRequest(
        `${apiBasePath}/fleet-groups/${id}/config`,
        configSectionsRequestSchema,
        { sections },
      ),
    }),

  // Async group apply: returns 202 with a batch id + per-agent job handles.
  // The caller then polls groupConfigApplyStatus with those handles.
  applyGroupConfig: (id: string) =>
    api<GroupApplyAccepted>(
      `${apiBasePath}/fleet-groups/${id}/config/apply`,
      { method: "POST" },
      groupApplyAcceptedSchema,
    ),

  // Poll the aggregate + per-agent status of an in-flight group apply. The
  // job handles (agent_id + job_id) are passed as paired repeated query
  // params so the backend can fold each target's job status; a no-op agent
  // (empty job_id) still rides along in order.
  groupConfigApplyStatus: (id: string, handles: readonly GroupApplyJobHandle[]) => {
    const params = new URLSearchParams();
    for (const h of handles) {
      params.append("agent", h.agent_id);
      params.append("job", h.job_id);
    }
    const query = params.toString();
    const suffix = query ? `?${query}` : "";
    return api<GroupApplyStatus>(
      `${apiBasePath}/fleet-groups/${id}/config/apply/status${suffix}`,
      undefined,
      groupApplyStatusSchema,
    );
  },

  // Persistent-batch aggregate for a single rollout, keyed by the batch id
  // POST .../config/apply returned. Unlike groupConfigApplyStatus above,
  // this is built entirely from the stored batch + target rows, so it can
  // be re-fetched after a browser reload or from a different device — the
  // whole point of Phase A persisting batches.
  getGroupConfigApplyBatch: (id: string, batchId: string) =>
    api<GroupApplyBatchStatus>(
      `${apiBasePath}/fleet-groups/${id}/config/apply/batches/${batchId}`,
      undefined,
      groupApplyBatchStatusSchema,
    ),

  // Resolves the fleet group's currently-running config-apply batch, if
  // any. This is the entry point a dashboard uses to discover whether
  // there is anything to resume-poll on mount, without needing to
  // remember a batch id across a page reload. The backend answers 204 No
  // Content (no body) when nothing is in flight; `api()` resolves that to
  // undefined before the schema is consulted, so the return type reflects
  // that honestly rather than lying about a guaranteed batch_id.
  activeGroupConfigApplyBatch: (id: string): Promise<GroupApplyActiveBatch | undefined> =>
    api(
      `${apiBasePath}/fleet-groups/${id}/config/apply/batches?active=1`,
      undefined,
      groupApplyActiveBatchSchema,
    ),
};
