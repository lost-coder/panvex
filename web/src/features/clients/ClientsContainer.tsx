import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";

import { type BulkClientAction, type ViewMode, EmptyState } from "@/ui";
import { ClientsPage } from "@/features/clients/ClientsPage";
import { SkeletonRows } from "@/ui";
import { useClientsList } from "./hooks/useClientsList";
import { useDiscoveredClients } from "./hooks/useDiscoveredClients";
import { useClientCreate } from "./hooks/useClientCreate";
import { useFleetGroups } from "@/features/servers/hooks/useFleetGroups";
import { useViewMode } from "@/shared/hooks/useViewMode";
import { useUrlSearchState } from "@/shared/hooks/useUrlSearchState";
import { useWsUpdateFlash } from "@/shared/hooks/useWsUpdateFlash";
import { apiClient } from "@/shared/api/api";
import { buildClientInput } from "@/shared/api/transforms/clients";

export function ClientsContainer() {
  const { clients, isLoading } = useClientsList();
  const { discoveredClients, groupCounts: discoveredGroupCounts } = useDiscoveredClients();
  const createMutation = useClientCreate();
  const { fleetGroups } = useFleetGroups();
  // Agents feed both the "pin individual node" selector and the fleet-group
  // count in the create sheet. Shared cache key with the servers page so
  // bouncing between /servers and /clients reuses the snapshot.
  const agentsQuery = useQuery({
    queryKey: ["agents"],
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
    () => fleetGroups.map((g) => ({ id: g.id, label: g.id, agentCount: g.agent_count })),
    [fleetGroups],
  );
  const { resolveMode, setMode } = useViewMode("clients");
  const navigate = useNavigate();
  const flashing = useWsUpdateFlash();
  const queryClient = useQueryClient();
  const [bulkError, setBulkError] = useState<string | undefined>();

  // Bulk dispatcher. Backend has no batch endpoint, so we fan out one
  // PUT/DELETE per client id and surface the first error verbatim. The
  // list query gets a single invalidate at the end so we don't thrash
  // /api/clients during a 100-row toggle.
  const bulkMutation = useMutation({
    mutationFn: async ({
      action,
      clientIds,
    }: {
      action: BulkClientAction;
      clientIds: string[];
    }) => {
      if (action === "delete") {
        await Promise.all(clientIds.map((id) => apiClient.deleteClient(id)));
        return;
      }
      // enable / disable: list endpoint only returns ClientListItem,
      // so we fetch each client individually to obtain the raw record
      // required by buildClientInput, then ship a full ClientInput
      // with `enabled` flipped (PUT /clients/:id is full-replacement,
      // not patch).
      const wantEnabled = action === "enable";
      await Promise.all(
        clientIds.map(async (id) => {
          const raw = await apiClient.client(id);
          if (raw.enabled === wantEnabled) return;
          const payload = buildClientInput(
            {
              name: raw.name,
              userAdTag: raw.user_ad_tag,
              expirationRfc3339: raw.expiration_rfc3339,
              maxTcpConns: raw.max_tcp_conns,
              maxUniqueIps: raw.max_unique_ips,
              dataQuotaBytes: raw.data_quota_bytes,
              fleetGroupIds: raw.fleet_group_ids ?? [],
              agentIds: raw.agent_ids ?? [],
            },
            { ...raw, enabled: wantEnabled },
          );
          await apiClient.updateClient(id, payload);
        }),
      );
    },
    onError: (err: unknown) =>
      setBulkError(err instanceof Error ? err.message : "Bulk action failed"),
    onSuccess: () => {
      setBulkError(undefined);
      queryClient.invalidateQueries({ queryKey: ["clients"] });
    },
  });

  // P2-UX-05: persist viewMode in the URL. localStorage (useViewMode)
  // still owns the long-lived preference; the URL copy is the sharable
  // layer so a deep-link lands peers in the same card/list state.
  const [viewParam, setViewParam] = useUrlSearchState("view", "");

  void discoveredClients; // keep the reference so the hook keeps revalidating
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

  // 2.5: empty state invites the first-time operator to create a
  // client instead of presenting a blank page that looks broken.
  if (clients.length === 0) {
    return (
      <div className="p-6">
        <EmptyState
          icon="👥"
          title="Клиентов пока нет"
          description="Создайте первого клиента, чтобы начать назначать его на ноды Telemt."
        />
      </div>
    );
  }

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
        onBulkAction={(action, clientIds) =>
          bulkMutation.mutateAsync({ action, clientIds })
        }
        bulkError={bulkError}
        bulkPending={bulkMutation.isPending}
      />
    </div>
  );
}
