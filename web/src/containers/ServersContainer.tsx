import { ServersPage, Spinner } from "@panvex/ui";
import { useServersList } from "@/hooks/useServersList";
import { useViewMode } from "@/hooks/useViewMode";
import { useNavigate } from "@tanstack/react-router";

export function ServersContainer() {
  const { servers, isLoading } = useServersList();
  const { resolveMode, setMode } = useViewMode("servers");
  const navigate = useNavigate();

  if (isLoading) {
    return <div className="flex items-center justify-center h-64"><Spinner /></div>;
  }

  return (
    <ServersPage
      servers={servers}
      viewMode={resolveMode(servers.length)}
      autoThreshold={10}
      onViewModeChange={setMode}
      onServerClick={(id) => navigate({ to: "/servers/$serverId", params: { serverId: id } })}
      onManageTokens={() => navigate({ to: "/servers/enrollment" })}
    />
  );
}
