import { useMemo } from "react";
import { DashboardPage, Spinner } from "@lost-coder/panvex-ui";
import { useDashboardData } from "@/hooks/useDashboardData";
import { useDiscoveredClients } from "@/hooks/useDiscoveredClients";
import { useClientCreate } from "@/hooks/useClientCreate";
import { useUpdates } from "@/hooks/useUpdates";
import { useNavigate } from "@tanstack/react-router";

export function DashboardContainer() {
  const { overview, timeline, agentVersions, isLoading } = useDashboardData();
  const { discoveredClients } = useDiscoveredClients();
  const createMutation = useClientCreate();
  const { query: updatesQuery } = useUpdates();
  const latestAgentVersion = updatesQuery.data?.state.latest_agent_version;
  const navigate = useNavigate();

  const pendingCount = discoveredClients.filter((c) => c.status === "pending_review").length;

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
    return <div className="flex items-center justify-center h-64"><Spinner /></div>;
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
    />
  );
}
