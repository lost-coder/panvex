import { api, apiBasePath, encodeRequest, type RequestOpts } from "./http";
import {
  agentCertificateRecoveryGrantRequestSchema,
  agentCertificateRecoverySchema,
  agentListSchema,
  agentSchema,
  dispatchTelemtUpdateRequestSchema,
  dispatchTelemtUpdateResponseSchema,
  instanceListSchema,
  provisionOutboundAgentRequestSchema,
  provisionOutboundAgentResponseSchema,
  renameAgentRequestSchema,
  telemtUpdateStrategySchema,
  telemtUpdateStrategyResponseSchema,
  updateAgentFleetGroupRequestSchema,
} from "./schemas";

import type { components } from "./openapi.gen.ts";
import type { LoosenOptional } from "./schemas/common.ts";

type Gen = components["schemas"];

// P8.3 (audit #23): presentation types derive from the generated OpenAPI
// types — openapi/panvex.yaml is the single source of truth. Runtime
// validation stays in schemas/agent.ts (Zod), whose parse output is
// compile-time bound to these same generated shapes via
// `satisfies z.ZodType<...>`, so `api<Agent[]>(…, agentListSchema)` below
// typechecks without casts.
//
// LoosenOptional wraps each gen type so an OPTIONAL field also admits
// `undefined` (recursively): the Zod parse output types absent fields as
// `T | undefined`, which under this repo's exactOptionalPropertyTypes is
// not assignable to a bare exact-optional `T?`. This is the same
// reconciliation the schema `satisfies` bindings use; it restores the
// shape the former hand-written types had (optionals carried `| undefined`)
// so consumers are unaffected.
export type Agent = LoosenOptional<Gen["Agent"]>;
export type AgentRuntime = LoosenOptional<Gen["AgentRuntime"]>;
export type RuntimeEvent = LoosenOptional<Gen["RuntimeEvent"]>;
export type AgentCertificateRecovery = LoosenOptional<
  Gen["AgentCertificateRecoveryGrant"]
>;

// PR-2c: response shape for POST /agents/provision-outbound. The
// wizard's outbound branch shows `command` verbatim and uses
// `agent_id` to poll for the first connection (and to call
// DELETE /agents/{id} on cancel).
export type ProvisionOutboundAgentResponse = LoosenOptional<
  Gen["ProvisionOutboundAgentResponse"]
>;

// Task 9/13: operator-configured Telemt update strategy + the agent's live
// probe. All fields on TelemtUpdateStrategy are required strings/enum on
// the wire (no optionals), so no LoosenOptional wrapping is needed there.
export type TelemtUpdateStrategy = Gen["TelemtUpdateStrategy"];
export type TelemtUpdateProbe = Gen["TelemtUpdateProbe"];
export type TelemtUpdateStrategyResponse = LoosenOptional<
  Gen["TelemtUpdateStrategyResponse"]
>;

// Task 14: 202 response of POST /agents/{id}/telemt/update — just the job
// id, no optionals to loosen.
export type DispatchTelemtUpdateResponse = Gen["DispatchTelemtUpdateResponse"];

// Instance has no OpenAPI counterpart yet (GET /api/instances is not in
// openapi/panvex.yaml) — kept hand-written. When the endpoint is specced,
// replace with Gen["Instance"].
export type Instance = {
  id: string;
  agent_id: string;
  name: string;
  version: string;
  config_fingerprint: string;
  connections: number;
  read_only: boolean;
  updated_at: string;
};

export const serversApi = {
  // R-Q-20: Zod parse on every response that carries a body.
  agents: (opts?: RequestOpts) =>
    api<Agent[]>(`${apiBasePath}/agents`, { signal: opts?.signal }, agentListSchema),
  instances: () =>
    api<Instance[]>(`${apiBasePath}/instances`, undefined, instanceListSchema),
  renameAgent: (agentID: string, nodeName: string) =>
    api<Agent>(
      `${apiBasePath}/agents/${agentID}`,
      {
        method: "PATCH",
        body: encodeRequest(
          `${apiBasePath}/agents/${agentID}`,
          renameAgentRequestSchema,
          { node_name: nodeName },
        ),
      },
      agentSchema,
    ),
  updateAgentFleetGroup: (agentID: string, fleetGroupID: string) =>
    api<Agent>(
      `${apiBasePath}/agents/${agentID}/fleet-group`,
      {
        method: "PUT",
        body: encodeRequest(
          `${apiBasePath}/agents/${agentID}/fleet-group`,
          updateAgentFleetGroupRequestSchema,
          { fleet_group_id: fleetGroupID },
        ),
      },
      agentSchema,
    ),
  deregisterAgent: (agentID: string) =>
    api<void>(`${apiBasePath}/agents/${agentID}`, {
      method: "DELETE"
    }),

  // PR-2c: provision an outbound (reverse-mode) agent and receive the
  // pre-baked curl|sudo-bash one-liner in a single round-trip. The
  // backend creates the agent row, mints a 5-minute bootstrap token,
  // and renders the install command honouring `script_source`
  // (defaults to "github" for outbound, since the panel is typically
  // firewalled from the agent host).
  provisionOutboundAgent: (payload: {
    node_name: string;
    fleet_group_id: string;
    dial_address: string;
    script_source?: "panel" | "github";
    advanced?: {
      telemt_url?: string | null;
      telemt_metrics_url?: string | null;
      telemt_auth?: string | null;
      insecure_transport?: boolean | null;
    };
  }) =>
    api<ProvisionOutboundAgentResponse>(
      `${apiBasePath}/agents/provision-outbound`,
      {
        method: "POST",
        body: encodeRequest(
          `${apiBasePath}/agents/provision-outbound`,
          provisionOutboundAgentRequestSchema,
          payload,
        ),
      },
      provisionOutboundAgentResponseSchema,
    ),
  allowAgentCertificateRecovery: (agentID: string, payload?: { ttl_seconds?: number }) =>
    api<AgentCertificateRecovery>(
      `${apiBasePath}/agents/${agentID}/certificate-recovery-grants`,
      {
        method: "POST",
        body: payload?.ttl_seconds
          ? encodeRequest(
              `${apiBasePath}/agents/${agentID}/certificate-recovery-grants`,
              agentCertificateRecoveryGrantRequestSchema,
              { ttl_seconds: payload.ttl_seconds },
            )
          : JSON.stringify({}),
      },
      agentCertificateRecoverySchema,
    ),
  revokeAgentCertificateRecovery: (agentID: string) =>
    api<AgentCertificateRecovery>(
      `${apiBasePath}/agents/${agentID}/certificate-recovery-grants/revoke`,
      { method: "POST" },
      agentCertificateRecoverySchema,
    ),

  // Task 9/13: GET returns both the persisted strategy (null when never
  // configured) and the agent's live probe (null until the agent has
  // reported one) in one round trip.
  getTelemtUpdateStrategy: (agentID: string, opts?: RequestOpts) =>
    api<TelemtUpdateStrategyResponse>(
      `${apiBasePath}/agents/${agentID}/telemt/update-strategy`,
      { signal: opts?.signal },
      telemtUpdateStrategyResponseSchema,
    ),
  putTelemtUpdateStrategy: (agentID: string, strategy: TelemtUpdateStrategy) =>
    api<void>(`${apiBasePath}/agents/${agentID}/telemt/update-strategy`, {
      method: "PUT",
      body: encodeRequest(
        `${apiBasePath}/agents/${agentID}/telemt/update-strategy`,
        telemtUpdateStrategySchema,
        strategy,
      ),
    }),

  // Task 11/14: dispatch an in-place Telemt binary update for one agent.
  // Empty version dispatches the panel's cached latest known release
  // (server-side default). 409s carry a typed `code` (see
  // dispatchTelemtUpdateErrorSchema) that the caller maps to a localized
  // toast via ApiError.code — no response-schema parse needed for the
  // error path, `api()` already extracts `code` from the JSON body.
  dispatchTelemtUpdate: (agentID: string, payload?: { version?: string; allowDowngrade?: boolean }) =>
    api<DispatchTelemtUpdateResponse>(
      `${apiBasePath}/agents/${agentID}/telemt/update`,
      {
        method: "POST",
        body: encodeRequest(
          `${apiBasePath}/agents/${agentID}/telemt/update`,
          dispatchTelemtUpdateRequestSchema,
          { version: payload?.version ?? "", allow_downgrade: payload?.allowDowngrade ?? false },
        ),
      },
      dispatchTelemtUpdateResponseSchema,
    ),
};
