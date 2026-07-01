import { api, apiBasePath, encodeRequest } from "./http";
import {
  agentConfigSchema,
  applyResultSchema,
  configSectionsRequestSchema,
  groupApplyAcceptedSchema,
  groupApplyStatusSchema,
  groupConfigSchema,
  type AgentConfig,
  type ApplyResult,
  type ConfigSections,
  type GroupApplyAccepted,
  type GroupApplyJobHandle,
  type GroupApplyStatus,
  type GroupConfig,
} from "./schemas";

export type {
  AgentConfig,
  ApplyResult,
  ConfigSections,
  GroupApplyAccepted,
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
};
