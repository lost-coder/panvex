import { ServerDetailPage, Spinner } from "@panvex/ui";
import { useServerDetail } from "@/hooks/useServerDetail";
import { useNavigate, useParams } from "@tanstack/react-router";

export function ServerDetailContainer() {
  const { serverId } = useParams({ strict: false });
  const { server, isLoading } = useServerDetail(serverId ?? "");
  const navigate = useNavigate();

  if (isLoading || !server) {
    return <div className="flex items-center justify-center h-64"><Spinner /></div>;
  }

  return (
    <ServerDetailPage
      server={server}
      onBack={() => navigate({ to: "/servers" })}
    />
  );
}
