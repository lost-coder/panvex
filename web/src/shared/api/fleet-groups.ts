import { api, apiBasePath } from "./http";
import {
  fleetGroupDeletionPreviewSchema,
  fleetGroupListSchema,
  fleetGroupSchema,
} from "./schemas";

export type FleetGroupIntegration = {
  id: string;
  kind: string;
  // R-Q-20: `| undefined` widens the optional shape so Zod schemas
  // line up with exactOptionalPropertyTypes.
  provider_id?: string | undefined;
  enabled: boolean;
  config: unknown;
  created_at_unix: number;
  updated_at_unix: number;
};

/**
 * FleetGroupEntry is the list/detail representation returned by
 * /api/fleet-groups and /api/fleet-groups/{id}. `integrations` is
 * only populated on the detail endpoint — list responses always
 * carry an empty array for payload consistency.
 */
export type FleetGroupEntry = {
  id: string;
  name: string;
  label: string;
  description: string;
  agent_count: number;
  created_at_unix: number;
  updated_at_unix: number;
  integrations: FleetGroupIntegration[];
};

export type CreateFleetGroupRequest = {
  name: string;
  label: string;
  description?: string;
};

export type UpdateFleetGroupRequest = {
  label: string;
  description?: string;
};

export type FleetGroupDeletionPreview = {
  id: string;
  agent_count: number;
  enrollment_token_count: number;
  client_assignment_count: number;
  reassign_required: boolean;
};

export type FleetGroupDeletionResult = {
  moved: FleetGroupDeletionPreview;
};

export type IntegrationKind = {
  name: string;
  description: string;
  provider_kind?: string;
};

export type IntegrationProviderKind = {
  name: string;
  description: string;
};

export type IntegrationProvider = {
  id: string;
  kind: string;
  label: string;
  config: unknown;
  created_at_unix: number;
  updated_at_unix: number;
};

export type CreateIntegrationProviderRequest = {
  kind: string;
  label: string;
  config: unknown;
};

export type UpdateIntegrationProviderRequest = {
  label: string;
  config: unknown;
};

export type InstallFleetGroupIntegrationRequest = {
  kind: string;
  provider_id?: string;
  enabled: boolean;
  config: unknown;
};

export type UpdateFleetGroupIntegrationRequest = {
  provider_id?: string;
  enabled: boolean;
  config: unknown;
};

export const fleetGroupsApi = {
  // R-Q-20: response Zod-parse on the read paths.
  fleetGroups: () =>
    api<FleetGroupEntry[]>(`${apiBasePath}/fleet-groups`, undefined, fleetGroupListSchema),
  fleetGroup: (id: string) =>
    api<FleetGroupEntry>(`${apiBasePath}/fleet-groups/${id}`, undefined, fleetGroupSchema),
  createFleetGroup: (payload: CreateFleetGroupRequest) =>
    api<FleetGroupEntry>(
      `${apiBasePath}/fleet-groups`,
      {
        method: "POST",
        body: JSON.stringify(payload),
      },
      fleetGroupSchema,
    ),
  updateFleetGroup: (id: string, payload: UpdateFleetGroupRequest) =>
    api<FleetGroupEntry>(
      `${apiBasePath}/fleet-groups/${id}`,
      {
        method: "PATCH",
        body: JSON.stringify(payload),
      },
      fleetGroupSchema,
    ),
  fleetGroupDeletionPreview: (id: string) =>
    api<FleetGroupDeletionPreview>(
      `${apiBasePath}/fleet-groups/${id}/deletion-preview`,
      undefined,
      fleetGroupDeletionPreviewSchema,
    ),
  // reassignTo is required when the preview reports reassign_required;
  // otherwise the backend returns 409. Callers should flow users
  // through a confirm dialog that picks a target group first.
  deleteFleetGroup: (id: string, reassignTo?: string) => {
    const qs = reassignTo ? `?reassign_to=${encodeURIComponent(reassignTo)}` : "";
    return api<FleetGroupDeletionResult>(`${apiBasePath}/fleet-groups/${id}${qs}`, {
      method: "DELETE",
    });
  },
  integrationKinds: () => api<IntegrationKind[]>(`${apiBasePath}/integration-kinds`),
  integrationProviderKinds: () => api<IntegrationProviderKind[]>(`${apiBasePath}/integration-provider-kinds`),
  integrationProviders: () => api<IntegrationProvider[]>(`${apiBasePath}/integration-providers`),
  integrationProvider: (id: string) =>
    api<IntegrationProvider>(`${apiBasePath}/integration-providers/${id}`),
  createIntegrationProvider: (payload: CreateIntegrationProviderRequest) =>
    api<IntegrationProvider>(`${apiBasePath}/integration-providers`, {
      method: "POST",
      body: JSON.stringify(payload),
    }),
  updateIntegrationProvider: (id: string, payload: UpdateIntegrationProviderRequest) =>
    api<IntegrationProvider>(`${apiBasePath}/integration-providers/${id}`, {
      method: "PATCH",
      body: JSON.stringify(payload),
    }),
  deleteIntegrationProvider: (id: string) =>
    api<void>(`${apiBasePath}/integration-providers/${id}`, { method: "DELETE" }),
  installFleetGroupIntegration: (
    fleetGroupID: string,
    payload: InstallFleetGroupIntegrationRequest,
  ) =>
    api<FleetGroupIntegration>(
      `${apiBasePath}/fleet-groups/${fleetGroupID}/integrations`,
      { method: "POST", body: JSON.stringify(payload) },
    ),
  fleetGroupIntegration: (fleetGroupID: string, integrationID: string) =>
    api<FleetGroupIntegration>(
      `${apiBasePath}/fleet-groups/${fleetGroupID}/integrations/${integrationID}`,
    ),
  updateFleetGroupIntegration: (
    fleetGroupID: string,
    integrationID: string,
    payload: UpdateFleetGroupIntegrationRequest,
  ) =>
    api<FleetGroupIntegration>(
      `${apiBasePath}/fleet-groups/${fleetGroupID}/integrations/${integrationID}`,
      { method: "PATCH", body: JSON.stringify(payload) },
    ),
  uninstallFleetGroupIntegration: (fleetGroupID: string, integrationID: string) =>
    api<void>(
      `${apiBasePath}/fleet-groups/${fleetGroupID}/integrations/${integrationID}`,
      { method: "DELETE" },
    ),
};
