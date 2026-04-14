import { ServersPage, Spinner } from "@lost-coder/panvex-ui";
import { useServersList } from "@/hooks/useServersList";
import { useFleetGroups } from "@/hooks/useFleetGroups";
import { useViewMode } from "@/hooks/useViewMode";
import { useNavigate } from "@tanstack/react-router";
import { ErrorState } from "@/components/ErrorState";

export function ServersContainer() {
  const { servers, isLoading, error } = useServersList();
  const { fleetGroups } = useFleetGroups();
  const { resolveMode, setMode } = useViewMode("servers");
  const navigate = useNavigate();

  if (isLoading) {
    return <div className="flex items-center justify-center h-64"><Spinner /></div>;
  }

  if (error) {
    return <ErrorState message={error.message} onRetry={() => window.location.reload()} />;
  }

  return (
    <ServersPage
      servers={servers}
      viewMode={resolveMode(servers.length)}
      autoThreshold={10}
      fleetGroups={fleetGroups.map((g) => ({ id: g.id, label: g.id, agentCount: g.agent_count }))}
      onViewModeChange={setMode}
      onServerClick={(id) => navigate({ to: "/servers/$serverId", params: { serverId: id } })}
      onManageTokens={() => navigate({ to: "/servers/enrollment" })}
    />
  );
}
