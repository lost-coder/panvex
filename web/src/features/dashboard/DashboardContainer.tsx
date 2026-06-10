import { useMemo } from "react";
import { SkeletonRows } from "@/ui";
import { DashboardPage } from "@/features/dashboard/DashboardPage";
import { useDashboardData } from "./hooks/useDashboardData";
import { useDiscoveredClients } from "@/features/clients/hooks/useDiscoveredClients";
import { useClientCreate } from "@/features/clients/hooks/useClientCreate";
import { useUpdates } from "@/shared/hooks/useUpdates";
import { useNavigate } from "@tanstack/react-router";

export function DashboardContainer() {
  const { overview, timeline, agentVersions, isLoading } = useDashboardData();
  const { groupCounts: discoveredGroupCounts } = useDiscoveredClients();
  const createMutation = useClientCreate();
  const { query: updatesQuery } = useUpdates();
  const latestAgentVersion = updatesQuery.data?.state.latest_agent_version;
  const navigate = useNavigate();

  // Logical-client count (dedup by clientName) instead of raw records.
  // Dashboard banner should read "137 discovered", not "548".
  const pendingCount = discoveredGroupCounts.pending;

  // Enrich dashboard nodes with update availability
  const enrichedOverview = useMemo(() => {
    if (!overview || !latestAgentVersion) return overview;
    const enrichNodes = <T extends { id: string }>(nodes: T[]) =>
      nodes.map((n) => ({
        ...n,
        updateAvailable:
          !!agentVersions[n.id] && agentVersions[n.id] !== latestAgentVersion,
      }));
    return {
      ...overview,
      attentionNodes: enrichNodes(overview.attentionNodes),
      healthyNodes: enrichNodes(overview.healthyNodes),
    };
  }, [overview, latestAgentVersion, agentVersions]);

  if (isLoading || !enrichedOverview || !timeline) {
    return (
      <div className="px-4 md:px-8 py-8">
        <SkeletonRows count={6} />
      </div>
    );
  }

  return (
    <DashboardPage
      overview={enrichedOverview}
      timeline={timeline}
      onNodeClick={(nodeId) => navigate({ to: "/servers/$serverId", params: { serverId: nodeId } })}
      onNodeLinkClick={(nodeId) => navigate({ to: "/servers/$serverId", params: { serverId: nodeId } })}
      onCreate={async (data) => { await createMutation.mutateAsync(data); }}
      createLoading={createMutation.isPending}
      createError={createMutation.error?.message}
      pendingDiscoveredCount={pendingCount}
      onDiscoveredClick={() => navigate({ to: "/clients/discovered" })}
      onViewAllServers={() => navigate({ to: "/servers" })}
      onAddServer={() => void navigate({ to: "/servers/add" })}
    />
  );
}
