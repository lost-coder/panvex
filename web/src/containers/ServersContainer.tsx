import { type ViewMode, Spinner } from "@lost-coder/panvex-ui";
import { ServersPage } from "@lost-coder/panvex-ui/pages";
import { useServersList } from "@/hooks/useServersList";
import { useFleetGroups } from "@/hooks/useFleetGroups";
import { useViewMode } from "@/hooks/useViewMode";
import { useUpdates } from "@/hooks/useUpdates";
import { useNavigate } from "@tanstack/react-router";
import { ErrorState } from "@/components/ErrorState";
import { useUrlSearchState } from "@/hooks/useUrlSearchState";
import { useWsUpdateFlash } from "@/hooks/useWsUpdateFlash";

export function ServersContainer() {
  const { servers, agentVersions, isLoading, error } = useServersList();
  const { fleetGroups } = useFleetGroups();
  const { resolveMode, setMode } = useViewMode("servers");
  const { query: updatesQuery } = useUpdates();
  const latestAgentVersion = updatesQuery.data?.state.latest_agent_version;
  const navigate = useNavigate();
  const flashing = useWsUpdateFlash();

  // P2-UX-05: persist viewMode in the URL so a shared link lands in the
  // same card/list mode. localStorage still holds the user's long-term
  // preference via useViewMode.
  const [viewParam, setViewParam] = useUrlSearchState("view", "");

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

  const urlView = viewParam === "cards" || viewParam === "list" ? (viewParam as ViewMode) : undefined;
  const effectiveMode = urlView ?? resolveMode(servers.length);

  return (
    <div className={flashing ? "transition-[box-shadow] duration-300 ring-2 ring-accent/20 rounded" : undefined}>
      <ServersPage
        servers={enrichedServers}
        viewMode={effectiveMode}
        autoThreshold={10}
        fleetGroups={fleetGroups.map((g) => ({ id: g.id, label: g.id, agentCount: g.agent_count }))}
        onViewModeChange={(m) => {
          setMode(m);
          setViewParam(m);
        }}
        onServerClick={(id) => navigate({ to: "/servers/$serverId", params: { serverId: id } })}
        onAddServer={() => navigate({ to: "/servers/add" })}
        onManageTokens={() => navigate({ to: "/servers/enrollment" })}
      />
    </div>
  );
}
