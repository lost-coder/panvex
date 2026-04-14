import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import type { DiscoveredClientItem } from "@lost-coder/panvex-ui";
import { apiClient } from "@/lib/api";
import { transformDiscoveredClientList } from "@/lib/transforms/discoveredClients";

export function useDiscoveredClients() {
  const queryClient = useQueryClient();

  const query = useQuery({
    queryKey: ["discovered-clients"],
    queryFn: () => apiClient.discoveredClients(),
    refetchInterval: 30_000,
  });

  const clients: DiscoveredClientItem[] = query.data
    ? transformDiscoveredClientList(query.data)
    : [];

  const adoptMutation = useMutation({
    mutationFn: (id: string) => apiClient.adoptDiscoveredClient(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["discovered-clients"] });
      queryClient.invalidateQueries({ queryKey: ["clients"] });
    },
  });

  const ignoreMutation = useMutation({
    mutationFn: (id: string) => apiClient.ignoreDiscoveredClient(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["discovered-clients"] });
    },
  });

  const adoptManyMutation = useMutation({
    mutationFn: async (ids: string[]) => {
      const results = await Promise.allSettled(
        ids.map((id) => apiClient.adoptDiscoveredClient(id)),
      );
      const failed = results.filter((r) => r.status === "rejected");
      if (failed.length > 0) {
        throw new Error(`Failed to adopt ${failed.length} of ${ids.length} clients`);
      }
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ["discovered-clients"] });
      queryClient.invalidateQueries({ queryKey: ["clients"] });
    },
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
  });

  return {
    discoveredClients: clients,
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
