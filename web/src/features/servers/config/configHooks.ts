// P5-T4: TanStack Query hooks for the agent/group config endpoints.
//
// Read queries follow the useServerDetail() style (queryKey + queryFn,
// guarded by `enabled`). Mutations mirror the clients-feature
// convention (useClientMutations): on failure they surface the error
// through the global toast channel, and on success they invalidate the
// matching config query so the UI refetches override/effective/drift.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { configApi } from "@/shared/api/config";
import type {
  ConfigSections,
  GroupApplyJobHandle,
} from "@/shared/api/schemas/config";
import { configKeys } from "@/features/servers/queryKeys";
import { useToast } from "@/app/providers/ToastProvider";

// How often the async group-apply status endpoint is polled while the
// rollout is still in flight. Mirrors the backend poll cadence closely
// enough that the operator sees per-agent progress promptly.
const GROUP_APPLY_POLL_MS = 1000;

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

// useApplyGroupConfig now KICKS OFF the async rollout: it POSTs and resolves
// with the 202 batch (batch id + per-agent job handles). It does NOT surface a
// success toast — the rollout is still in flight — but it does surface an
// enqueue failure. The caller feeds the returned handles to
// useGroupConfigApplyStatus to poll per-agent progress.
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

// useGroupConfigApplyStatus polls the async group-apply status endpoint until
// every target is terminal (done === true), then stops refetching. Enabled
// only while a batch is active with at least one non-no-op job handle; the
// query is keyed by batchId so a fresh Apply starts a clean poll cycle. On
// success/refetch the group config query is invalidated so the drift summary
// re-renders as agents converge.
export function useGroupConfigApplyStatus(
  groupId: string,
  batchId: string | null,
  handles: readonly GroupApplyJobHandle[],
) {
  const qc = useQueryClient();
  const hasWork = handles.some((h) => h.job_id !== "");
  return useQuery({
    queryKey: configKeys.groupApplyStatus(groupId, batchId ?? ""),
    queryFn: async () => {
      const status = await configApi.groupConfigApplyStatus(groupId, handles);
      void qc.invalidateQueries({ queryKey: configKeys.group(groupId) });
      return status;
    },
    enabled: !!groupId && !!batchId && hasWork,
    // Poll while in flight; stop once the aggregate reports done.
    refetchInterval: (query) =>
      query.state.data?.done ? false : GROUP_APPLY_POLL_MS,
  });
}

// useGroupConfigApplyBatch polls the PERSISTENT batch-status endpoint
// (GET .../config/apply/batches/{batchId}) until the batch reaches a
// terminal status. This is what makes the rollout view resumable: the
// aggregate is rebuilt from the stored batch + target rows rather than the
// job handles the client happened to receive from the 202 response, so it
// survives a page reload, a panel restart, or being opened from a different
// device. Mirrors useGroupConfigApplyStatus's refetchInterval-until-done
// pattern; the group config query is invalidated on each settle so the
// per-node drift summary converges alongside the rollout.
export function useGroupConfigApplyBatch(groupId: string, batchId: string | null) {
  const qc = useQueryClient();
  return useQuery({
    queryKey: configKeys.groupApplyBatch(groupId, batchId ?? ""),
    queryFn: async () => {
      // `enabled` guards batchId being non-null before this ever runs.
      const status = await configApi.getGroupConfigApplyBatch(groupId, batchId as string);
      void qc.invalidateQueries({ queryKey: configKeys.group(groupId) });
      return status;
    },
    enabled: !!groupId && !!batchId,
    refetchInterval: (query) =>
      query.state.data?.done ? false : GROUP_APPLY_POLL_MS,
  });
}

// useActiveGroupConfigApplyBatch resolves the fleet group's currently
// running config-apply batch (if any) on mount/groupId change. This is the
// RESUME entry point: instead of a page reload wiping the batch id that
// used to live only in React state, the section asks "is anything in
// flight for this group right now" and, if so, feeds the resolved id to
// useGroupConfigApplyBatch to drive the same per-agent panel. React
// Query's default staleTime (0) means this refetches on every mount,
// which is exactly the "reload re-fetches via the active endpoint, not
// React state" behaviour the resumable view needs.
export function useActiveGroupConfigApplyBatch(groupId: string) {
  return useQuery({
    queryKey: configKeys.activeGroupApplyBatch(groupId),
    // React Query rejects `undefined` as query data (it reads as "no data
    // fetched yet"), so the "no batch in flight" case is normalized to
    // `null` here rather than propagating the API's `undefined`.
    queryFn: async () => (await configApi.activeGroupConfigApplyBatch(groupId)) ?? null,
    enabled: !!groupId,
  });
}
