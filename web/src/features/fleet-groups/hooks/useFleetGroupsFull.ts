import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { apiClient } from "@/shared/api/api";
import {
  fleetGroupsKeys,
  integrationKindsKeys,
  integrationProviderKindsKeys,
  integrationProvidersKeys,
} from "@/features/fleet-groups/queryKeys";
import { agentsKeys } from "@/features/servers/queryKeys";
import { useEventAwareInterval } from "@/shared/hooks/useEventAwareInterval";
import type {
  CreateFleetGroupRequest,
  UpdateFleetGroupRequest,
  InstallFleetGroupIntegrationRequest,
  UpdateFleetGroupIntegrationRequest,
  CreateIntegrationProviderRequest,
  UpdateIntegrationProviderRequest,
} from "@/shared/api/api";

// List of fleet groups, full shape. Separate from the legacy
// useFleetGroups in features/servers/hooks which shares the same
// endpoint but ships an older expectation set (id + agent_count);
// we keep both so migration can roll forward per-consumer instead
// of a big-bang rewrite.
export function useFleetGroupsList() {
  const groupsInterval = useEventAwareInterval(90_000, 30_000);

  return useQuery({
    queryKey: fleetGroupsKeys.list(),
    queryFn: ({ signal }) => apiClient.fleetGroups({ signal }),
    refetchInterval: groupsInterval,
  });
}

// Detail query includes integrations[]. Skipped while id is empty
// so the page can keep its loading state while the router settles.
export function useFleetGroupDetail(id: string | undefined) {
  const groupDetailInterval = useEventAwareInterval(60_000, 15_000);

  return useQuery({
    queryKey: fleetGroupsKeys.detail(id),
    queryFn: ({ signal }) => {
      if (!id) throw new Error("fleet group id is required");
      return apiClient.fleetGroup(id, { signal });
    },
    enabled: !!id,
    refetchInterval: groupDetailInterval,
  });
}

// Deletion-preview runs before the actual DELETE so the UI can show
// how many agents / tokens / assignments will be reassigned and
// force the operator to pick a target group.
export function useFleetGroupDeletionPreview(id: string | undefined, enabled = true) {
  return useQuery({
    queryKey: fleetGroupsKeys.deletionPreview(id),
    queryFn: ({ signal }) => {
      if (!id) throw new Error("fleet group id is required");
      return apiClient.fleetGroupDeletionPreview(id, { signal });
    },
    enabled: !!id && enabled,
    // Preview is an operator-triggered read; no polling.
    refetchInterval: false,
  });
}

export function useFleetGroupMutations() {
  const qc = useQueryClient();

  const createMutation = useMutation({
    mutationFn: (payload: CreateFleetGroupRequest) => apiClient.createFleetGroup(payload),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: fleetGroupsKeys.all });
    },
  });

  const updateMutation = useMutation({
    mutationFn: ({ id, payload }: { id: string; payload: UpdateFleetGroupRequest }) =>
      apiClient.updateFleetGroup(id, payload),
    onSuccess: (_data, variables) => {
      void qc.invalidateQueries({ queryKey: fleetGroupsKeys.all });
      void qc.invalidateQueries({ queryKey: fleetGroupsKeys.detail(variables.id) });
    },
  });

  const deleteMutation = useMutation({
    mutationFn: ({ id, reassignTo }: { id: string; reassignTo?: string | undefined }) =>
      apiClient.deleteFleetGroup(id, reassignTo),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: fleetGroupsKeys.all });
      // Invalidate the agents list too — agents may have been moved
      // to the reassignTo group as part of the delete flow. BP-02:
      // pulls `agentsKeys` from the servers feature instead of a
      // bare ["agents"] literal so cache identity stays canonical.
      void qc.invalidateQueries({ queryKey: agentsKeys.all });
    },
  });

  return { createMutation, updateMutation, deleteMutation };
}

// ---- Integrations scaffolding -----------------------------------------

export function useIntegrationKinds() {
  return useQuery({
    queryKey: integrationKindsKeys.list(),
    queryFn: ({ signal }) => apiClient.integrationKinds({ signal }),
    // Kinds are registry-driven and change at control-plane boot
    // only; a 10-minute stale window keeps the hook well-behaved
    // without real-time refresh.
    staleTime: 10 * 60 * 1000,
    refetchInterval: false,
  });
}

export function useIntegrationProviderKinds() {
  return useQuery({
    queryKey: integrationProviderKindsKeys.list(),
    queryFn: ({ signal }) => apiClient.integrationProviderKinds({ signal }),
    staleTime: 10 * 60 * 1000,
    refetchInterval: false,
  });
}

export function useIntegrationProvidersList() {
  const providersInterval = useEventAwareInterval(300_000, 60_000);

  return useQuery({
    queryKey: integrationProvidersKeys.list(),
    queryFn: ({ signal }) => apiClient.integrationProviders({ signal }),
    refetchInterval: providersInterval,
  });
}

export function useIntegrationProviderMutations() {
  const qc = useQueryClient();

  const createMutation = useMutation({
    mutationFn: (payload: CreateIntegrationProviderRequest) =>
      apiClient.createIntegrationProvider(payload),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: integrationProvidersKeys.all });
    },
  });

  // Write-only secret contract (H-6): the API redacts provider secret
  // fields (e.g. cloudflare api_token) to "" on every read, so the
  // value never reaches the client. When a provider-management form is
  // built, it MUST:
  //   - render secret fields as <input type="password">;
  //   - show a placeholder like "leave blank to keep current";
  //   - submit an empty secret to keep the stored value (the backend
  //     merges empty secrets back from the sealed config — keep-on-empty),
  //     and a non-empty value to overwrite it.
  // Non-secret fields (account_id) round-trip normally.
  // TODO(integrations-ui): no provider-management form exists yet; this
  // hook is the data layer it will consume.
  const updateMutation = useMutation({
    mutationFn: ({ id, payload }: { id: string; payload: UpdateIntegrationProviderRequest }) =>
      apiClient.updateIntegrationProvider(id, payload),
    onSuccess: (_data, variables) => {
      void qc.invalidateQueries({ queryKey: integrationProvidersKeys.all });
      void qc.invalidateQueries({ queryKey: integrationProvidersKeys.detail(variables.id) });
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (id: string) => apiClient.deleteIntegrationProvider(id),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: integrationProvidersKeys.all });
    },
  });

  return { createMutation, updateMutation, deleteMutation };
}

// Per-group integration mutations. Takes the fleet-group id at hook
// construction so invalidations hit the correct detail cache key.
export function useFleetGroupIntegrationMutations(fleetGroupID: string) {
  const qc = useQueryClient();
  const invalidate = () => {
    void qc.invalidateQueries({ queryKey: fleetGroupsKeys.detail(fleetGroupID) });
    void qc.invalidateQueries({ queryKey: fleetGroupsKeys.all });
  };

  const installMutation = useMutation({
    mutationFn: (payload: InstallFleetGroupIntegrationRequest) =>
      apiClient.installFleetGroupIntegration(fleetGroupID, payload),
    onSuccess: invalidate,
  });

  const updateMutation = useMutation({
    mutationFn: ({
      integrationID,
      payload,
    }: {
      integrationID: string;
      payload: UpdateFleetGroupIntegrationRequest;
    }) => apiClient.updateFleetGroupIntegration(fleetGroupID, integrationID, payload),
    onSuccess: invalidate,
  });

  const uninstallMutation = useMutation({
    mutationFn: (integrationID: string) =>
      apiClient.uninstallFleetGroupIntegration(fleetGroupID, integrationID),
    onSuccess: invalidate,
  });

  return { installMutation, updateMutation, uninstallMutation };
}
