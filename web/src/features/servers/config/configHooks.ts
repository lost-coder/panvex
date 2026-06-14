// P5-T4: TanStack Query hooks for the agent/group config endpoints.
//
// Read queries follow the useServerDetail() style (queryKey + queryFn,
// guarded by `enabled`). Mutations mirror the clients-feature
// convention (useClientMutations): on failure they surface the error
// through the global toast channel, and on success they invalidate the
// matching config query so the UI refetches override/effective/drift.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { configApi } from "@/shared/api/config";
import type { ConfigSections } from "@/shared/api/schemas/config";
import { configKeys } from "@/features/servers/queryKeys";
import { useToast } from "@/app/providers/ToastProvider";

export function useAgentConfig(agentId: string) {
  return useQuery({
    queryKey: configKeys.agent(agentId),
    queryFn: () => configApi.getAgentConfig(agentId),
    enabled: !!agentId,
  });
}

export function useGroupConfig(groupId: string) {
  return useQuery({
    queryKey: configKeys.group(groupId),
    queryFn: () => configApi.getGroupConfig(groupId),
    enabled: !!groupId,
  });
}

export function usePutAgentConfig(agentId: string) {
  const qc = useQueryClient();
  const toast = useToast();
  return useMutation({
    mutationFn: (sections: ConfigSections) => configApi.putAgentConfig(agentId, sections),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: configKeys.agent(agentId) });
    },
    onError: (err: Error) => {
      toast.error(err.message);
    },
  });
}

export function useApplyAgentConfig(agentId: string) {
  const qc = useQueryClient();
  const toast = useToast();
  return useMutation({
    mutationFn: () => configApi.applyAgentConfig(agentId),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: configKeys.agent(agentId) });
    },
    onError: (err: Error) => {
      toast.error(err.message);
    },
  });
}

export function usePutGroupConfig(groupId: string) {
  const qc = useQueryClient();
  const toast = useToast();
  return useMutation({
    mutationFn: (sections: ConfigSections) => configApi.putGroupConfig(groupId, sections),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: configKeys.group(groupId) });
    },
    onError: (err: Error) => {
      toast.error(err.message);
    },
  });
}

export function useApplyGroupConfig(groupId: string) {
  const qc = useQueryClient();
  const toast = useToast();
  return useMutation({
    mutationFn: () => configApi.applyGroupConfig(groupId),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: configKeys.group(groupId) });
    },
    onError: (err: Error) => {
      toast.error(err.message);
    },
  });
}
