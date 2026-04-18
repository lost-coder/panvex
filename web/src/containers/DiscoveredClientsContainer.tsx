import { Spinner } from "@lost-coder/panvex-ui";
import { DiscoveredClientsPage } from "@lost-coder/panvex-ui/pages";
import { useDiscoveredClients } from "@/hooks/useDiscoveredClients";
import { useNavigate } from "@tanstack/react-router";
import { ErrorState } from "@/components/ErrorState";
import { useConfirm } from "@/providers/ConfirmProvider";
import { useUrlSearchState } from "@/hooks/useUrlSearchState";

export function DiscoveredClientsContainer() {
  const { discoveredClients, isLoading, error, adopt, ignore, adoptMany, ignoreMany, isAdopting, isIgnoring } =
    useDiscoveredClients();
  const navigate = useNavigate();
  const confirm = useConfirm();

  // P2-UX-05: persist a coarse "which tab am I on" URL param so operators
  // can share a link mid-triage. The DiscoveredClientsPage owns the
  // actual filter UI internally; tying it into the URL is left to a
  // follow-up ticket (see deferred notes in commit body).
  const [filterParam] = useUrlSearchState("filter", "");
  void filterParam;

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

  // P2-UX-04: bulk-ignore is destructive per candidate — confirm only
  // the multi-select variant. Single-row ignore stays click-through
  // because operators triage this list one row at a time.
  const handleIgnoreMany = async (ids: string[]) => {
    if (ids.length === 0) return;
    const ok = await confirm({
      title: `Ignore ${ids.length} discovered ${ids.length === 1 ? "client" : "clients"}?`,
      body: "Ignored candidates are filtered out of future scans until re-discovered.",
      confirmLabel: "Ignore",
      variant: "danger",
    });
    if (!ok) return;
    await ignoreMany(ids);
  };

  return (
    <DiscoveredClientsPage
      clients={discoveredClients}
      onAdopt={(id) => adopt(id)}
      onIgnore={(id) => ignore(id)}
      onAdoptMany={(ids: string[]) => adoptMany(ids)}
      onIgnoreMany={handleIgnoreMany}
      onBack={() => navigate({ to: "/clients" })}
      busy={isAdopting || isIgnoring}
    />
  );
}
