import { api, apiBasePath, encodeRequest } from "./http";
import {
  agentConfigSchema,
  applyResultSchema,
  configSectionsRequestSchema,
  groupConfigSchema,
  type AgentConfig,
  type ApplyResult,
  type ConfigSections,
  type GroupConfig,
} from "./schemas";

export type { AgentConfig, ApplyResult, ConfigSections, GroupConfig };

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

  applyGroupConfig: (id: string) =>
    api<ApplyResult>(
      `${apiBasePath}/fleet-groups/${id}/config/apply`,
      { method: "POST" },
      applyResultSchema,
    ),
};
