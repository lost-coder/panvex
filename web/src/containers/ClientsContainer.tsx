import { ClientsPage, Spinner } from "@lost-coder/panvex-ui";
import { useClientsList } from "@/hooks/useClientsList";
import { useDiscoveredClients } from "@/hooks/useDiscoveredClients";
import { useClientCreate } from "@/hooks/useClientCreate";
import { useViewMode } from "@/hooks/useViewMode";
import { useNavigate } from "@tanstack/react-router";

export function ClientsContainer() {
  const { clients, isLoading } = useClientsList();
  const { discoveredClients } = useDiscoveredClients();
  const createMutation = useClientCreate();
  const { resolveMode, setMode } = useViewMode("clients");
  const navigate = useNavigate();

  const pendingCount = discoveredClients.filter((dc) => dc.status === "pending_review").length;

  if (isLoading) {
    return <div className="flex items-center justify-center h-64"><Spinner /></div>;
  }

  return (
    <ClientsPage
      clients={clients}
      viewMode={resolveMode(clients.length)}
      autoThreshold={10}
      onViewModeChange={setMode}
      onClientClick={(id) => navigate({ to: "/clients/$clientId", params: { clientId: id } })}
      onCreate={async (data) => { await createMutation.mutateAsync(data); }}
      createLoading={createMutation.isPending}
      createError={createMutation.error?.message}
      pendingDiscoveredCount={pendingCount}
      onDiscoveredClick={() => navigate({ to: "/clients/discovered" })}
    />
  );
}
