import { DiscoveredClientsPage, Spinner } from "@lost-coder/panvex-ui";
import { useDiscoveredClients } from "@/hooks/useDiscoveredClients";
import { useNavigate } from "@tanstack/react-router";
import { ErrorState } from "@/components/ErrorState";

export function DiscoveredClientsContainer() {
  const { discoveredClients, isLoading, error, adopt, ignore, adoptMany, ignoreMany, isAdopting, isIgnoring } =
    useDiscoveredClients();
  const navigate = useNavigate();

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Spinner />
      </div>
    );
  }

  if (error) {
    return <ErrorState message={error.message} onRetry={() => window.location.reload()} />;
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
