import { ClientsPage, Spinner } from "@panvex/ui";
import { useClientsList } from "@/hooks/useClientsList";
import { useViewMode } from "@/hooks/useViewMode";
import { useNavigate } from "@tanstack/react-router";

export function ClientsContainer() {
  const { clients, isLoading } = useClientsList();
  const { resolveMode, setMode } = useViewMode("clients");
  const navigate = useNavigate();

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
    />
  );
}
