import { api, apiBasePath, encodeRequest } from "./http";
import type { Job } from "./jobs";
import {
  adoptDiscoveredClientResponseSchema,
  bulkAdoptDiscoveredRequestSchema,
  bulkAdoptDiscoveredResponseSchema,
  bulkClientActionRequestSchema,
  bulkClientResponseSchema,
  clientIPHistoryResponseSchema,
  clientListSchema,
  clientMutationRequestSchema,
  clientSchema,
  discoveredClientListSchema,
  resetQuotaResponseSchema,
  rescanDiscoveredResponseSchema,
  type RescanDiscoveredResponse,
} from "./schemas";

export type ClientListItem = {
  id: string;
  name: string;
  enabled: boolean;
  assigned_nodes_count: number;
  expiration_rfc3339: string;
  traffic_used_bytes: number;
  unique_ips_used: number;
  active_tcp_conns: number;
  data_quota_bytes: number;
  last_deploy_status: string;
};

export type ClientDeployment = {
  agent_id: string;
  desired_operation: string;
  status: string;
  last_error: string;
  connection_links: string[];
  last_applied_at_unix: number;
  updated_at_unix: number;
  // Phase 1 of the reset-quota plan. Optional on the wire so consumers
  // that don't care don't pay a strict-typing cost; the zod parse
  // path defaults missing values to 0 (see schemas/client.ts).
  quota_used_bytes?: number | undefined;
  quota_last_reset_unix?: number | undefined;
  // Phase 3 of the reset-quota plan. panel_last_reset_unix is the
  // panel's record of the last successful reset for this pair, and
  // quota_reset_drift is true when the panel's record is newer than
  // Telemt's reported quota_last_reset_unix (the reset job succeeded
  // but Telemt's persisted state has fallen behind). Optional for
  // wire-compat with older backends.
  panel_last_reset_unix?: number | undefined;
  quota_reset_drift?: boolean | undefined;
};

export type Client = {
  id: string;
  name: string;
  secret: string;
  user_ad_tag: string;
  enabled: boolean;
  traffic_used_bytes: number;
  unique_ips_used: number;
  active_tcp_conns: number;
  max_tcp_conns: number;
  max_unique_ips: number;
  data_quota_bytes: number;
  expiration_rfc3339: string;
  fleet_group_ids: string[];
  agent_ids: string[];
  deployments: ClientDeployment[];
  created_at_unix: number;
  updated_at_unix: number;
  deleted_at_unix: number;
};

export type ClientInput = {
  name: string;
  enabled?: boolean;
  user_ad_tag?: string;
  /**
   * Tri-state flag. Omitted (or `true`) keeps the legacy auto-
   * generation: if `user_ad_tag` is empty the control-plane mints a
   * fresh 32-hex value. Set to `false` to store the value literally
   * — empty means the client gets no ad tag at all.
   */
  user_ad_tag_auto?: boolean;
  max_tcp_conns: number;
  max_unique_ips: number;
  data_quota_bytes: number;
  expiration_rfc3339: string;
  fleet_group_ids: string[];
  agent_ids: string[];
};

export type DiscoveredClientConflict = {
  type: "same_secret_different_names" | "same_name_different_secrets";
  related_ids: string[];
};

export type DiscoveredClient = {
  id: string;
  agent_id: string;
  node_name: string;
  client_name: string;
  status: "pending_review" | "adopted" | "ignored";
  total_octets: number;
  current_connections: number;
  active_unique_ips: number;
  connection_links: string[];
  max_tcp_conns: number;
  max_unique_ips: number;
  data_quota_bytes: number;
  expiration: string;
  discovered_at_unix: number;
  updated_at_unix: number;
  conflicts?: DiscoveredClientConflict[] | undefined;
};

export type AdoptDiscoveredClientResponse = {
  client_id: string;
  name: string;
};

export type BulkAdoptResultStatus = "adopted" | "already_adopted" | "error";

export type BulkAdoptResult = {
  id: string;
  status: BulkAdoptResultStatus;
  // R-Q-20: `| undefined` widens the optional shape so Zod schemas
  // line up with exactOptionalPropertyTypes.
  client_id?: string | undefined;
  name?: string | undefined;
  message?: string | undefined;
};

export type BulkAdoptDiscoveredResponse = {
  results: BulkAdoptResult[];
  adopted_count: number;
  already_adopted_count: number;
  error_count: number;
  // R-Q-20: `| undefined` widens the optional shape so Zod schemas
  // line up with exactOptionalPropertyTypes.
  skipped_out_of_scope?: number | undefined;
};

export type ClientIPEntry = {
  ip_address: string;
  first_seen: string;
  last_seen: string;
  country_code?: string | undefined;
  country_name?: string | undefined;
  city?: string | undefined;
  asn?: string | undefined;
};

export type ClientIPHistoryResponse = {
  ips: ClientIPEntry[];
  total_unique: number;
};

export type BulkClientServerAction = "enable" | "disable" | "delete";

export type BulkClientFailure = {
  id: string;
  error: string;
};

export type BulkClientResponse = {
  action: BulkClientServerAction;
  succeeded: string[];
  skipped: string[];
  failed: BulkClientFailure[];
};

/**
 * Reset-quota Phase 2 wire shape: backend enqueues the reset job and
 * replies with the refreshed client detail plus the new Job. Callers
 * watch `job.id` until each target's status reaches a terminal value
 * (`succeeded` / `failed` / `expired`) and then parse `result_json` for
 * the typed unsupported / read-only / success payload.
 */
export type ResetQuotaResponse = {
  client: Client;
  job: Job;
};

export const clientsApi = {
  // R-Q-20: Zod parse on every response that carries a body.
  clients: () => api<ClientListItem[]>(`${apiBasePath}/clients`, undefined, clientListSchema),
  client: (clientID: string) =>
    api<Client>(`${apiBasePath}/clients/${clientID}`, undefined, clientSchema),
  createClient: (payload: ClientInput) =>
    api<Client>(
      `${apiBasePath}/clients`,
      {
        method: "POST",
        body: encodeRequest(`${apiBasePath}/clients`, clientMutationRequestSchema, payload),
      },
      clientSchema,
    ),
  updateClient: (clientID: string, payload: ClientInput) =>
    api<Client>(
      `${apiBasePath}/clients/${clientID}`,
      {
        method: "PUT",
        body: encodeRequest(
          `${apiBasePath}/clients/${clientID}`,
          clientMutationRequestSchema,
          payload,
        ),
      },
      clientSchema,
    ),
  rotateClientSecret: (clientID: string) =>
    api<Client>(
      `${apiBasePath}/clients/${clientID}/rotate-secret`,
      { method: "POST" },
      clientSchema,
    ),
  // Re-runs the client.create rollout for every target agent — used
  // to recover a client whose initial deployment failed on at least
  // one node. Backend reuses the stored client state, so callers do
  // not need to re-send form fields.
  redeployClient: (clientID: string) =>
    api<Client>(
      `${apiBasePath}/clients/${clientID}/redeploy`,
      { method: "POST" },
      clientSchema,
    ),
  deleteClient: (clientID: string) =>
    api<void>(`${apiBasePath}/clients/${clientID}`, {
      method: "DELETE"
    }),
  discoveredClients: () =>
    api<DiscoveredClient[]>(
      `${apiBasePath}/discovered-clients`,
      undefined,
      discoveredClientListSchema,
    ),
  adoptDiscoveredClient: (id: string) =>
    api<AdoptDiscoveredClientResponse>(
      `${apiBasePath}/discovered-clients/${id}/adopt`,
      { method: "POST" },
      adoptDiscoveredClientResponseSchema,
    ),
  // Bulk adopt: server processes the whole list under one rate-limit
  // token and folds duplicate-by-(name, secret) discovered records into
  // the same managed client automatically. The frontend no longer
  // needs to PUT agent_ids back or fire per-id ignore calls — that
  // dance was the source of both the rate-limit cascade and the
  // accidental ad_tag auto-generation triggered by the follow-up PUT.
  bulkAdoptDiscoveredClients: (ids: string[]) =>
    api<BulkAdoptDiscoveredResponse>(
      `${apiBasePath}/discovered-clients/bulk-adopt`,
      {
        method: "POST",
        body: encodeRequest(
          `${apiBasePath}/discovered-clients/bulk-adopt`,
          bulkAdoptDiscoveredRequestSchema,
          { ids },
        ),
      },
      bulkAdoptDiscoveredResponseSchema,
    ),
  ignoreDiscoveredClient: (id: string) =>
    api<void>(`${apiBasePath}/discovered-clients/${id}/ignore`, {
      method: "POST"
    }),
  rescanDiscoveredClients: () =>
    api<RescanDiscoveredResponse>(
      `${apiBasePath}/discovered-clients/rescan`,
      { method: "POST" },
      rescanDiscoveredResponseSchema,
    ),
  // Single-call bulk variant: replaces the previous N-PUT/N-DELETE
  // fan-out from the dashboard with one authoritative POST. Capped at
  // 500 ids by the server; the UI typically operates on far fewer.
  bulkClientAction: (action: BulkClientServerAction, ids: string[]) =>
    api<BulkClientResponse>(
      `${apiBasePath}/clients/bulk-action`,
      {
        method: "POST",
        body: encodeRequest(
          `${apiBasePath}/clients/bulk-action`,
          bulkClientActionRequestSchema,
          { action, ids },
        ),
      },
      bulkClientResponseSchema,
    ),
  /**
   * Reset-quota Phase 2 — fan-out variant. Resets the per-agent quota
   * counter on every agent currently hosting the client. The backend
   * enqueues one `reset_client_quota` job spanning all targets and
   * returns immediately; the caller watches `result.job.id` against
   * /api/jobs to surface per-target outcome.
   */
  resetClientQuotaFanOut: (clientID: string) =>
    api<ResetQuotaResponse>(
      `${apiBasePath}/clients/${clientID}/reset-quota`,
      { method: "POST" },
      resetQuotaResponseSchema,
    ),
  /**
   * Reset-quota Phase 2 — single-agent variant. Same job pipeline as
   * the fan-out, scoped to one node. Useful when only one deployment
   * is misbehaving and the operator wants to avoid touching counters
   * on the rest of the fleet.
   */
  resetClientQuotaOnAgent: (clientID: string, agentID: string) =>
    api<ResetQuotaResponse>(
      `${apiBasePath}/clients/${clientID}/reset-quota/${agentID}`,
      { method: "POST" },
      resetQuotaResponseSchema,
    ),
  clientIPHistory: (clientID: string, from?: string, to?: string) => {
    const params = new URLSearchParams();
    if (from) params.set("from", from);
    if (to) params.set("to", to);
    const qs = params.toString();
    return api<ClientIPHistoryResponse>(
      `${apiBasePath}/clients/${clientID}/history/ips${qs ? "?" + qs : ""}`,
      undefined,
      clientIPHistoryResponseSchema,
    );
  },
};
