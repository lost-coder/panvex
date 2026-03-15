export type MeResponse = {
  id: string;
  username: string;
  role: string;
};

export type FleetResponse = {
  total_agents: number;
  online_agents: number;
  degraded_agents: number;
  offline_agents: number;
  total_instances: number;
  metric_snapshots: number;
};

export type Agent = {
  id: string;
  node_name: string;
  environment_id: string;
  fleet_group_id: string;
  version: string;
  read_only: boolean;
  last_seen_at: string;
};

export type Instance = {
  id: string;
  agent_id: string;
  name: string;
  version: string;
  config_fingerprint: string;
  connected_users: number;
  read_only: boolean;
  updated_at: string;
};

export type Job = {
  id: string;
  action: string;
  target_agent_ids: string[];
  ttl: number;
  idempotency_key: string;
  actor_id: string;
  status: string;
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

export type MetricSnapshot = {
  id: string;
  agent_id: string;
  instance_id: string;
  captured_at: string;
  values: Record<string, number>;
};

export type EnrollmentTokenResponse = {
  value: string;
  environment_id: string;
  fleet_group_id: string;
  issued_at_unix: number;
  expires_at_unix: number;
  ca_pem: string;
};

export type JobCreateInput = {
  action: string;
  target_agent_ids: string[];
  idempotency_key: string;
  ttl_seconds: number;
};

const apiBasePath = "/api";

export async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
      ...(init?.headers ?? {})
    },
    ...init
  });

  if (response.status === 204) {
    return undefined as T;
  }

  if (!response.ok) {
    let message = `Request failed with status ${response.status}`;
    try {
      const payload = (await response.json()) as { error?: string };
      if (payload.error) {
        message = payload.error;
      }
    } catch {
      // Ignore JSON parsing failures for error responses.
    }
    throw new Error(message);
  }

  return (await response.json()) as T;
}

export const apiClient = {
  login: (payload: { username: string; password: string; totp_code?: string }) =>
    api<{ session_id: string }>(`${apiBasePath}/auth/login`, {
      method: "POST",
      body: JSON.stringify(payload)
    }),
  logout: () =>
    api<void>(`${apiBasePath}/auth/logout`, {
      method: "POST"
    }),
  me: () => api<MeResponse>(`${apiBasePath}/auth/me`),
  fleet: () => api<FleetResponse>(`${apiBasePath}/fleet`),
  agents: () => api<Agent[]>(`${apiBasePath}/agents`),
  instances: () => api<Instance[]>(`${apiBasePath}/instances`),
  jobs: () => api<Job[]>(`${apiBasePath}/jobs`),
  audit: () => api<AuditEvent[]>(`${apiBasePath}/audit`),
  metrics: () => api<MetricSnapshot[]>(`${apiBasePath}/metrics`),
  createJob: (payload: JobCreateInput) =>
    api<Job>(`${apiBasePath}/jobs`, {
      method: "POST",
      body: JSON.stringify(payload)
    }),
  createEnrollmentToken: (payload: {
    environment_id: string;
    fleet_group_id: string;
    ttl_seconds: number;
  }) =>
    api<EnrollmentTokenResponse>(`${apiBasePath}/agents/enrollment-tokens`, {
      method: "POST",
      body: JSON.stringify(payload)
    })
};
