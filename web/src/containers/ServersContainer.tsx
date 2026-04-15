import { ServersPage, Spinner } from "@lost-coder/panvex-ui";
import { useServersList } from "@/hooks/useServersList";
import { useFleetGroups } from "@/hooks/useFleetGroups";
import { useViewMode } from "@/hooks/useViewMode";
import { useUpdates } from "@/hooks/useUpdates";
import { useNavigate } from "@tanstack/react-router";
import { ErrorState } from "@/components/ErrorState";

export function ServersContainer() {
  const { servers, agentVersions, isLoading, error } = useServersList();
  const { fleetGroups } = useFleetGroups();
  const { resolveMode, setMode } = useViewMode("servers");
  const { query: updatesQuery } = useUpdates();
  const latestAgentVersion = updatesQuery.data?.state.latest_agent_version;
  const navigate = useNavigate();

  if (isLoading) {
    return <div className="flex items-center justify-center h-64"><Spinner /></div>;
  }

  if (error) {
    return <ErrorState message={error.message} onRetry={() => window.location.reload()} />;
  }

  // Enrich servers with update availability when latest version is known
  const enrichedServers = latestAgentVersion
    ? servers.map((s) => ({
        ...s,
        updateAvailable:
          !!agentVersions[s.id] && agentVersions[s.id] !== latestAgentVersion,
      }))
    : servers;

  return (
    <ServersPage
      servers={enrichedServers}
      viewMode={resolveMode(servers.length)}
      autoThreshold={10}
      fleetGroups={fleetGroups.map((g) => ({ id: g.id, label: g.id, agentCount: g.agent_count }))}
      onViewModeChange={setMode}
      onServerClick={(id) => navigate({ to: "/servers/$serverId", params: { serverId: id } })}
      onAddServer={() => navigate({ to: "/servers/add" })}
      onManageTokens={() => navigate({ to: "/servers/enrollment" })}
    />
  );
}
