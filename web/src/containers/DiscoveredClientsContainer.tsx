import { DiscoveredClientsPage, Spinner } from "@panvex/ui";
import { useDiscoveredClients } from "@/hooks/useDiscoveredClients";
import { useNavigate } from "@tanstack/react-router";

export function DiscoveredClientsContainer() {
  const { discoveredClients, isLoading, adopt, ignore, adoptMany, ignoreMany, isAdopting, isIgnoring } =
    useDiscoveredClients();
  const navigate = useNavigate();

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Spinner />
      </div>
    );
  }

  return (
    <DiscoveredClientsPage
      clients={discoveredClients}
      onAdopt={(id) => adopt(id)}
      onIgnore={(id) => ignore(id)}
      onAdoptMany={(ids: string[]) => adoptMany(ids)}
      onIgnoreMany={(ids: string[]) => ignoreMany(ids)}
      onBack={() => navigate({ to: "/clients" })}
      busy={isAdopting || isIgnoring}
    />
  );
}
