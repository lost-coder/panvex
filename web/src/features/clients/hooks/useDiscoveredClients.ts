import { useMemo } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import type { DiscoveredClientItem } from "@/shared/api/types-pages/pages";
import type { DiscoveredClient } from "@/shared/api/api";
import { apiClient } from "@/shared/api/api";
import { transformDiscoveredClientList } from "@/shared/api/transforms/discoveredClients";
import {
  countDiscoveredGroups,
  type DiscoveredGroupCounts,
} from "@/features/clients/lib/groupDiscovered";
import { useToast } from "@/app/providers/ToastProvider";

/**
 * Adopt / ignore flow notes
 * =========================
 * The backend returns one discovered record per (client, node), so the
 * same logical Telemt client shows up N times — once per node it exists
 * on. The single-id `adopt` endpoint creates a managed client scoped
 * only to the agent whose record was passed in, which leaves the client
 * deployed to a subset of the nodes it was actually running on.
 *
 * Until backend-followup #6 lands proper server-side dedup, we do
 * correct fan-out here:
 *   1. Adopt the first record → backend creates a managed client
 *      attached to agent #1.
 *   2. Fetch the managed client by id so we can PUT a full ClientInput
 *      (the update endpoint is full-replacement, not patch).
 *   3. Merge in every agent_id from the other records in the group
 *      and PUT the managed client so it lands on all of them.
 *   4. Ignore the remaining discovered records so they don't linger
 *      in the pending-review surface — they're duplicates of the same
 *      logical client we just adopted.
 *
 * `adopt` (single) stays untouched — the UI calls it only for groups
 * of size 1.
 */
export function useDiscoveredClients() {
  const queryClient = useQueryClient();
  // Each mutation here is fire-and-forget from the container, so failures
  // need to land in the toast channel or the operator has no signal that
  // the button they clicked actually hit an error.
  const toast = useToast();

  const query = useQuery({
    queryKey: ["discovered-clients"],
    queryFn: () => apiClient.discoveredClients(),
    refetchInterval: 30_000,
  });

  // R-Q-24: memoise the transformed list so the useMemo at line 179
  // gets a stable reference whenever query.data does not change. Without
  // this, every render rebuilds `clients` and forces every downstream
  // memo to recompute.
  const clients: DiscoveredClientItem[] = useMemo(
    () => (query.data ? transformDiscoveredClientList(query.data) : []),
    [query.data],
  );

  const collectAgentIds = (ids: string[]): string[] => {
    const raw = queryClient.getQueryData<DiscoveredClient[]>(["discovered-clients"]) ?? [];
    const found = new Set<string>();
    for (const id of ids) {
      const rec = raw.find((r) => r.id === id);
      if (rec?.agent_id) found.add(rec.agent_id);
    }
    return Array.from(found);
  };

  const adoptMutation = useMutation({
    mutationFn: (id: string) => apiClient.adoptDiscoveredClient(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["discovered-clients"] });
      queryClient.invalidateQueries({ queryKey: ["clients"] });
    },
    onError: (err: Error) => toast.error(`Adopt failed: ${err.message}`),
  });

  const ignoreMutation = useMutation({
    mutationFn: (id: string) => apiClient.ignoreDiscoveredClient(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["discovered-clients"] });
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

      // Collect every agent_id the group covers BEFORE adopting, so the
      // adopt response doesn't remove the rows we're about to look up.
      const agentIds = collectAgentIds(ids);

      // 1. Adopt the first record. Backend returns the managed client id.
      const first = await apiClient.adoptDiscoveredClient(ids[0]!);

      // 2. Pull the fresh managed client so we have the full shape. Steps
      //    2 and 3 are wrapped so a failure here surfaces as "adopted but
      //    partial" rather than a generic mutation error — the operator
      //    needs to know the client exists and is scoped narrowly.
      let managed;
      try {
        managed = await apiClient.client(first.client_id);
      } catch (err) {
        throw new Error(
          `Adopted 1 record but could not fetch managed client to merge other ${ids.length - 1} agent(s): ${err instanceof Error ? err.message : String(err)}`,
        );
      }

      // 3. Merge agent_ids and PUT the managed client back. Skip the PUT
      //    when the adopt already wrote everything we wanted.
      const mergedAgentIds = Array.from(
        new Set([...(managed.agent_ids ?? []), ...agentIds]),
      );
      const hasAllAgents =
        mergedAgentIds.length === (managed.agent_ids ?? []).length;
      if (!hasAllAgents) {
        try {
          await apiClient.updateClient(first.client_id, {
            name: managed.name,
            enabled: managed.enabled,
            user_ad_tag: managed.user_ad_tag,
            max_tcp_conns: managed.max_tcp_conns,
            max_unique_ips: managed.max_unique_ips,
            data_quota_bytes: managed.data_quota_bytes,
            expiration_rfc3339: managed.expiration_rfc3339,
            fleet_group_ids: managed.fleet_group_ids ?? [],
            agent_ids: mergedAgentIds,
          });
        } catch (err) {
          throw new Error(
            `Adopted client but failed to extend agent_ids — client is scoped only to 1 node: ${err instanceof Error ? err.message : String(err)}`,
          );
        }
      }

      // 4. Ignore the remaining discovered records so they clear out of
      //    pending review — they're duplicates of the just-adopted client.
      //    Use allSettled because a single stray failure here shouldn't
      //    roll back the successful adoption above; we still surface a
      //    warning toast if any reject.
      const ignored = await Promise.allSettled(
        ids.slice(1).map((id) => apiClient.ignoreDiscoveredClient(id)),
      );
      const staleIgnores = ignored.filter((r) => r.status === "rejected").length;
      if (staleIgnores > 0) {
        toast.info(
          `Adopted, but ${staleIgnores} duplicate record${staleIgnores === 1 ? "" : "s"} could not be cleared — refresh to retry.`,
        );
      }
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ["discovered-clients"] });
      queryClient.invalidateQueries({ queryKey: ["clients"] });
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
      queryClient.invalidateQueries({ queryKey: ["discovered-clients"] });
    },
    onError: (err: Error) => toast.error(err.message),
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
    adopt: adoptMutation.mutateAsync,
    ignore: ignoreMutation.mutateAsync,
    adoptMany: adoptManyMutation.mutateAsync,
    ignoreMany: ignoreManyMutation.mutateAsync,
    isAdopting: adoptMutation.isPending || adoptManyMutation.isPending,
    isIgnoring: ignoreMutation.isPending || ignoreManyMutation.isPending,
  };
}
