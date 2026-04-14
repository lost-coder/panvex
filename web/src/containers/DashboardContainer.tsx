import { DashboardPage, Spinner } from "@lost-coder/panvex-ui";
import { useDashboardData } from "@/hooks/useDashboardData";
import { useDiscoveredClients } from "@/hooks/useDiscoveredClients";
import { useClientCreate } from "@/hooks/useClientCreate";
import { useNavigate } from "@tanstack/react-router";

export function DashboardContainer() {
  const { overview, timeline, isLoading } = useDashboardData();
  const { discoveredClients } = useDiscoveredClients();
  const createMutation = useClientCreate();
  const navigate = useNavigate();

  const pendingCount = discoveredClients.filter((c) => c.status === "pending_review").length;

  if (isLoading || !overview || !timeline) {
    return <div className="flex items-center justify-center h-64"><Spinner /></div>;
  }

  return (
    <DashboardPage
      overview={overview}
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
