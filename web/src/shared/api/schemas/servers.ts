/**
 * Aggregator module for the servers (a.k.a. Agent / Node) endpoint
 * family. Q-08: the `/api/agents/*` HTTP routes back the React
 * "Servers" pages, hence the dual name. The actual response/request
 * Zod schemas live in their domain-specific files (agent.ts,
 * instance schema, request shapes) — this module exists so call
 * sites and the BP-02 audit can find a single
 * `schemas/servers.ts` peer to `api/servers.ts`, matching the
 * one-file-per-endpoint-family convention used elsewhere in this
 * folder.
 */

export {
  agentCertificateRecoverySchema,
  agentListSchema,
  agentRuntimeSchema,
  agentSchema,
  instanceListSchema,
  instanceSchema,
  type AgentCertificateRecoveryParsed,
  type AgentParsed,
  type InstanceParsed,
} from "./agent.ts";
export {
  agentCertificateRecoveryGrantRequestSchema,
  type AgentCertificateRecoveryGrantRequest,
} from "./requests/agentCertificateRecoveryGrantRequest.ts";
export {
  renameAgentRequestSchema,
  type RenameAgentRequest,
} from "./requests/renameAgentRequest.ts";
export {
  updateAgentFleetGroupRequestSchema,
  type UpdateAgentFleetGroupRequest,
} from "./requests/updateAgentFleetGroupRequest.ts";
