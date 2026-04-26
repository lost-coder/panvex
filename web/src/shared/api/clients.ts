import { api, apiBasePath, encodeRequest } from "./http";
import {
  clientListSchema,
  clientMutationRequestSchema,
  discoveredClientListSchema,
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
  connection_link: string;
  last_applied_at_unix: number;
  updated_at_unix: number;
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
  connection_link: string;
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

export type ClientIPEntry = {
  ip_address: string;
  first_seen: string;
  last_seen: string;
};

export type ClientIPHistoryResponse = {
  ips: ClientIPEntry[];
  total_unique: number;
};

export const clientsApi = {
  clients: () => api<ClientListItem[]>(`${apiBasePath}/clients`, undefined, clientListSchema),
  client: (clientID: string) => api<Client>(`${apiBasePath}/clients/${clientID}`),
  createClient: (payload: ClientInput) =>
    api<Client>(`${apiBasePath}/clients`, {
      method: "POST",
      body: encodeRequest(`${apiBasePath}/clients`, clientMutationRequestSchema, payload),
    }),
  updateClient: (clientID: string, payload: ClientInput) =>
    api<Client>(`${apiBasePath}/clients/${clientID}`, {
      method: "PUT",
      body: encodeRequest(
        `${apiBasePath}/clients/${clientID}`,
        clientMutationRequestSchema,
        payload,
      ),
    }),
  rotateClientSecret: (clientID: string) =>
    api<Client>(`${apiBasePath}/clients/${clientID}/rotate-secret`, {
      method: "POST"
    }),
  // Re-runs the client.create rollout for every target agent — used
  // to recover a client whose initial deployment failed on at least
  // one node. Backend reuses the stored client state, so callers do
  // not need to re-send form fields.
  redeployClient: (clientID: string) =>
    api<Client>(`${apiBasePath}/clients/${clientID}/redeploy`, {
      method: "POST"
    }),
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
    api<AdoptDiscoveredClientResponse>(`${apiBasePath}/discovered-clients/${id}/adopt`, {
      method: "POST"
    }),
  ignoreDiscoveredClient: (id: string) =>
    api<void>(`${apiBasePath}/discovered-clients/${id}/ignore`, {
      method: "POST"
    }),
  clientIPHistory: (clientID: string, from?: string, to?: string) => {
    const params = new URLSearchParams();
    if (from) params.set("from", from);
    if (to) params.set("to", to);
    const qs = params.toString();
    return api<ClientIPHistoryResponse>(`${apiBasePath}/clients/${clientID}/history/ips${qs ? "?" + qs : ""}`);
  },
};
