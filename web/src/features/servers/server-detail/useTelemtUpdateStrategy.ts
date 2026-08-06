// Task 13: TanStack Query hooks for the per-agent Telemt update strategy
// (GET/PUT /api/agents/{id}/telemt/update-strategy, Task 9). Mirrors the
// config-feature hooks convention (see servers/config/configHooks.ts):
// the read side is a plain useQuery, the write side toasts on failure and
// invalidates the read query on success — the caller decides whether to
// also show a success toast (see TelemtUpdateSection's handleSave).

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiClient, type TelemtUpdateStrategy } from "@/shared/api/api";
import { telemtUpdateKeys } from "@/features/servers/queryKeys";
import { useToast } from "@/app/providers/ToastProvider";

export function useTelemtUpdateStrategy(agentId: string) {
  return useQuery({
    queryKey: telemtUpdateKeys.strategy(agentId),
    queryFn: ({ signal }) => apiClient.getTelemtUpdateStrategy(agentId, { signal }),
    enabled: !!agentId,
  });
}

export function usePutTelemtUpdateStrategy(agentId: string) {
  const qc = useQueryClient();
  const toast = useToast();
  return useMutation({
    mutationFn: (strategy: TelemtUpdateStrategy) =>
      apiClient.putTelemtUpdateStrategy(agentId, strategy),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: telemtUpdateKeys.strategy(agentId) });
    },
    onError: (err: Error) => {
      toast.error(err.message);
    },
  });
}
