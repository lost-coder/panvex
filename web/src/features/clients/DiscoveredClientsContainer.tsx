import { DiscoveredClientsPage } from "./DiscoveredClientsPage";
import { useDiscoveredClients } from "./hooks/useDiscoveredClients";
import { useNavigate } from "@tanstack/react-router";
import { ErrorState } from "@/components/ErrorState";
import { SkeletonRows } from "@/components/Skeleton";
import { useConfirm } from "@/app/providers/ConfirmProvider";
import { useUrlSearchState } from "@/shared/hooks/useUrlSearchState";

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
      <div className="px-4 md:px-8 py-8">
        <SkeletonRows count={5} />
      </div>
    );
  }

  if (error) {
    return <ErrorState message={error.message} onRetry={() => window.location.reload()} />;
  }

  // Ignore (single and bulk) is destructive — ignored candidates
  // vanish from the pending-review surface until they're rediscovered.
  // Adopt is non-destructive, but adopt-many often spans dozens of
  // records after the front-end dedup fanout, so we confirm the
  // fanout too.
  const handleIgnoreOne = async (id: string) => {
    const ok = await confirm({
      title: "Ignore this discovered client?",
      body: "It will not appear in pending review unless reset.",
      confirmLabel: "Ignore",
      variant: "danger",
    });
    if (!ok) return;
    await ignore(id);
  };
  const handleIgnoreMany = async (ids: string[]) => {
    if (ids.length === 0) return;
    const ok = await confirm({
      title: `Ignore ${ids.length} discovered record${ids.length === 1 ? "" : "s"}?`,
      body: "Ignored candidates are filtered out of future scans until re-discovered.",
      confirmLabel: "Ignore",
      variant: "danger",
    });
    if (!ok) return;
    await ignoreMany(ids);
  };
  const handleAdoptMany = async (ids: string[]) => {
    if (ids.length === 0) return;
    // Front-end dedup fans one logical client into N records (one per
    // node). Confirm the fanout so the operator sees the scale.
    const ok = await confirm({
      title: `Adopt ${ids.length} record${ids.length === 1 ? "" : "s"}?`,
      body:
        ids.length === 1
          ? "Import the discovered client as a managed one."
          : `Adopt-many will fan out ${ids.length} records (one per node they were discovered on) so the resulting managed clients are registered on every node.`,
      confirmLabel: "Adopt",
      variant: "default",
    });
    if (!ok) return;
    await adoptMany(ids);
  };

  return (
    <DiscoveredClientsPage
      clients={discoveredClients}
      onAdopt={(id) => adopt(id)}
      onIgnore={handleIgnoreOne}
      onAdoptMany={handleAdoptMany}
      onIgnoreMany={handleIgnoreMany}
      onBack={() => navigate({ to: "/clients" })}
      busy={isAdopting || isIgnoring}
    />
  );
}
