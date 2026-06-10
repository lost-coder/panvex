import { useMemo } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import type { DiscoveredClientItem } from "@/shared/api/types-pages/pages";
import { apiClient } from "@/shared/api/api";
import { transformDiscoveredClientList } from "@/shared/api/transforms/discoveredClients";
import {
  countDiscoveredGroups,
  type DiscoveredGroupCounts,
} from "@/features/clients/lib/groupDiscovered";
import { useToast } from "@/app/providers/ToastProvider";
import { clientsKeys } from "@/features/clients/queryKeys";
import { useEventAwareInterval } from "@/shared/hooks/useEventAwareInterval";

/**
 * Adopt / ignore flow
 * ===================
 * The backend returns one discovered record per (client, node), so the
 * same logical Telemt client shows up N times. Bulk-adopt now pushes
 * the whole list to a single backend endpoint, which:
 *   - groups records by (name, secret) and folds every node's agent_id
 *     into one managed client on creation;
 *   - flips sibling discovered rows to "adopted" in the same
 *     transaction (no per-id ignore round-trip);
 *   - consumes one rate-limit token for the whole batch.
 *
 * The previous client-side fan-out (adopt → GET → PUT agent_ids →
 * ignore the rest) blew the sensitive rate-limiter and accidentally
 * triggered ad_tag auto-generation on the trailing PUT. Both classes
 * of bug are gone with the bulk path.
 */
export function useDiscoveredClients() {
  const queryClient = useQueryClient();
  const { t } = useTranslation("clients");
  // Each mutation here is fire-and-forget from the container, so failures
  // need to land in the toast channel or the operator has no signal that
  // the button they clicked actually hit an error.
  const toast = useToast();

  const refetchInterval = useEventAwareInterval(90_000, 30_000);

  const query = useQuery({
    queryKey: clientsKeys.discovered,
    queryFn: () => apiClient.discoveredClients(),
    refetchInterval,
  });

  // R-Q-24: memoise the transformed list so the useMemo at line 179
  // gets a stable reference whenever query.data does not change. Without
  // this, every render rebuilds `clients` and forces every downstream
  // memo to recompute.
  const clients: DiscoveredClientItem[] = useMemo(
    () => (query.data ? transformDiscoveredClientList(query.data) : []),
    [query.data],
  );

  const adoptMutation = useMutation({
    mutationFn: (id: string) => apiClient.adoptDiscoveredClient(id),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: clientsKeys.discovered });
      void queryClient.invalidateQueries({ queryKey: clientsKeys.all });
    },
    onError: (err: Error) => toast.error(`Adopt failed: ${err.message}`),
  });

  const ignoreMutation = useMutation({
    mutationFn: (id: string) => apiClient.ignoreDiscoveredClient(id),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: clientsKeys.discovered });
    },
    onError: (err: Error) => toast.error(`Ignore failed: ${err.message}`),
  });

  const adoptManyMutation = useMutation({
    mutationFn: async (ids: string[]) => {
      if (ids.length === 0) return;
      if (ids.length === 1) {
        await apiClient.adoptDiscoveredClient(ids[0]!);
        return;
      }
      const resp = await apiClient.bulkAdoptDiscoveredClients(ids);
      // Surface partial failures as a warning so operators can investigate
      // without losing the successful adopts. The backend already covers
      // sibling records via the duplicate-flip, so already_adopted is the
      // expected outcome for every-id-after-first within a logical group
      // — those should NOT be flagged as failures.
      if (resp.error_count > 0) {
        const firstErr = resp.results.find((r) => r.status === "error");
        const detail = firstErr?.message ? `: ${firstErr.message}` : "";
        toast.error(
          `Adopted ${resp.adopted_count} client${resp.adopted_count === 1 ? "" : "s"}, but ${resp.error_count} record${resp.error_count === 1 ? "" : "s"} failed${detail}`,
        );
      } else if (resp.adopted_count > 0) {
        toast.info(
          `Adopted ${resp.adopted_count} client${resp.adopted_count === 1 ? "" : "s"}.`,
        );
      }
    },
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: clientsKeys.discovered });
      void queryClient.invalidateQueries({ queryKey: clientsKeys.all });
    },
    onError: (err: Error) => toast.error(err.message),
  });

  const ignoreManyMutation = useMutation({
    mutationFn: async (ids: string[]) => {
      const results = await Promise.allSettled(
        ids.map((id) => apiClient.ignoreDiscoveredClient(id)),
      );
      const failed = results.filter((r) => r.status === "rejected");
      if (failed.length > 0) {
        throw new Error(`Failed to ignore ${failed.length} of ${ids.length} clients`);
      }
    },
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: clientsKeys.discovered });
    },
    onError: (err: Error) => toast.error(err.message),
  });

  const rescanMutation = useMutation({
    mutationFn: () => apiClient.rescanDiscoveredClients(),
    onSuccess: (res) => {
      toast.success(t("discovered.rescan.success", { count: res.agents_notified }));
      void queryClient.invalidateQueries({ queryKey: clientsKeys.discovered });
    },
    onError: () => toast.error(t("discovered.rescan.error")),
  });

  // Logical-client counts derived from the dedupe grouping. Consumers
  // (Dashboard banner, Clients list banner, the discovered page itself)
  // should use these instead of `clients.filter(...).length` — the raw
  // array carries one record per (client, node) so the filtered count
  // is always N× what the operator thinks of as "one client".
  const groupCounts: DiscoveredGroupCounts = useMemo(
    () => countDiscoveredGroups(clients),
    [clients],
  );

  return {
    discoveredClients: clients,
    groupCounts,
    isLoading: query.isLoading,
    error: query.error,
    refetch: query.refetch,
    adopt: adoptMutation.mutateAsync,
    ignore: ignoreMutation.mutateAsync,
    adoptMany: adoptManyMutation.mutateAsync,
    ignoreMany: ignoreManyMutation.mutateAsync,
    rescan: () => rescanMutation.mutateAsync(),
    isAdopting: adoptMutation.isPending || adoptManyMutation.isPending,
    isIgnoring: ignoreMutation.isPending || ignoreManyMutation.isPending,
    isRescanning: rescanMutation.isPending,
  };
}
