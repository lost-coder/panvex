import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";

import { type BulkClientAction, type ViewMode } from "@/ui";
import { ClientsPage } from "@/features/clients/ClientsPage";
import { SkeletonRows } from "@/ui";
import { useClientsList } from "./hooks/useClientsList";
import { useDiscoveredClients } from "./hooks/useDiscoveredClients";
import { useClientCreate } from "./hooks/useClientCreate";
import { clientsKeys } from "@/features/clients/queryKeys";
import { useFleetGroups } from "@/features/servers/hooks/useFleetGroups";
import { agentsKeys } from "@/features/servers/queryKeys";
import { useViewMode } from "@/shared/hooks/useViewMode";
import { useUrlSearchState } from "@/shared/hooks/useUrlSearchState";
import { useWsUpdateFlash } from "@/shared/hooks/useWsUpdateFlash";
import { apiClient } from "@/shared/api/api";

export function ClientsContainer() {
  const { clients, isLoading } = useClientsList();
  const { groupCounts: discoveredGroupCounts } = useDiscoveredClients();
  const createMutation = useClientCreate();
  const { fleetGroups } = useFleetGroups();
  // Agents feed both the "pin individual node" selector and the fleet-group
  // count in the create sheet. Shared cache key with the servers page so
  // bouncing between /servers and /clients reuses the snapshot.
  // BP-02 audit: pulls `agentsKeys` from the servers feature instead of a
  // bare `["agents"]` literal — the leak made it impossible to track which
  // surface invalidates which key.
  const agentsQuery = useQuery({
    queryKey: agentsKeys.list(),
    queryFn: () => apiClient.agents(),
    staleTime: 30_000,
  });
  const agentOptions = useMemo(
    () =>
      (agentsQuery.data ?? []).map((a) => ({
        id: a.id,
        nodeName: a.node_name || a.id,
        fleetGroupId: a.fleet_group_id,
        online: a.presence_state === "online",
      })),
    [agentsQuery.data],
  );
  const fleetGroupOptions = useMemo(
    () => fleetGroups.map((g) => ({ id: g.id, label: g.label || g.name || g.id, agentCount: g.agent_count })),
    [fleetGroups],
  );
  const { resolveMode, setMode } = useViewMode("clients");
  const navigate = useNavigate();
  const flashing = useWsUpdateFlash();
  const queryClient = useQueryClient();
  const [bulkError, setBulkError] = useState<string | undefined>();

  // Bulk dispatcher: single call to /clients/bulk-action replaces the
  // previous one-PUT-per-client fan-out (~200 round-trips on a 100-row
  // toggle). The list query gets a single invalidate when the call
  // returns. Per-id failures come back in the response body as
  // {failed: [{id, error}]} so the operator sees which rows did not
  // apply rather than swallowing the first error.
  const bulkMutation = useMutation({
    mutationFn: async ({
      action,
      clientIds,
    }: {
      action: BulkClientAction;
      clientIds: string[];
    }) => apiClient.bulkClientAction(action, clientIds),
    onError: (err: unknown) =>
      setBulkError(err instanceof Error ? err.message : "Bulk action failed"),
    onSuccess: (response) => {
      setBulkError(
        response.failed.length > 0
          ? `${response.failed.length.toString()} client(s) failed: ${response.failed
              .slice(0, 3)
              .map((f) => f.error)
              .join("; ")}`
          : undefined,
      );
      void queryClient.invalidateQueries({ queryKey: clientsKeys.all });
    },
  });

  // P2-UX-05: persist viewMode in the URL. localStorage (useViewMode)
  // still owns the long-lived preference; the URL copy is the sharable
  // layer so a deep-link lands peers in the same card/list state.
  const [viewParam, setViewParam] = useUrlSearchState("view", "");

  // Pending count is the logical-client number (groupCounts.pending),
  // not the raw record count. A 137-client × 4-node fleet reports 137
  // here, not 548. See src/features/clients/lib/groupDiscovered.ts.
  const pendingCount = discoveredGroupCounts.pending;

  if (isLoading) {
    // 2.5: skeleton instead of a full-height spinner so the layout
    // slot matches the eventual list and there is no content flash
    // when the data arrives.
    return (
      <div className="p-4">
        <SkeletonRows count={6} />
      </div>
    );
  }

  const urlView = viewParam === "cards" || viewParam === "list" ? (viewParam as ViewMode) : undefined;
  const effectiveMode = urlView ?? resolveMode(clients.length);

  // Empty-state copy lives inside ClientsPage — rendering ClientsPage
  // unconditionally keeps the PageHeader's "Add Client" button
  // visible so the first-time operator actually has a CTA instead of
  // a dead-end placeholder.

  return (
    // P2-UX-10: subtle ring flash when the underlying query revalidates
    // due to a WS event. Transition is short (1.2s) so it doesn't linger.
    <div className={flashing ? "transition-[box-shadow] duration-300 ring-2 ring-accent/20 rounded" : undefined}>
      <ClientsPage
        clients={clients}
        viewMode={effectiveMode}
        autoThreshold={10}
        onViewModeChange={(m) => {
          setMode(m);
          setViewParam(m);
        }}
        onClientClick={(id) => navigate({ to: "/clients/$clientId", params: { clientId: id } })}
        onCreate={async (data) => { await createMutation.mutateAsync(data); }}
        createLoading={createMutation.isPending}
        createError={createMutation.error?.message}
        fleetGroups={fleetGroupOptions}
        agents={agentOptions}
        pendingDiscoveredCount={pendingCount}
        onDiscoveredClick={() => navigate({ to: "/clients/discovered" })}
        onBulkAction={async (action, clientIds) => {
          await bulkMutation.mutateAsync({ action, clientIds });
        }}
        bulkError={bulkError}
        bulkPending={bulkMutation.isPending}
      />
    </div>
  );
}
