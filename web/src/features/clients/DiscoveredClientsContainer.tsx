import { useCallback } from "react";
import { DiscoveredClientsPage } from "./DiscoveredClientsPage";
import { useDiscoveredClients } from "./hooks/useDiscoveredClients";
import { useNavigate } from "@tanstack/react-router";
import { ErrorState } from "@/components/ErrorState";
import { SkeletonRows } from "@/ui";
import { useConfirm } from "@/app/providers/ConfirmProvider";
import { useUrlSearchState } from "@/shared/hooks/useUrlSearchState";
import { groupDiscovered } from "./lib/groupDiscovered";

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
  // Q3.U-P-18: wrap the async handlers in useCallback so DiscoveredClientsPage
  // (and any memoised children) keep stable function identities across
  // re-renders. Without this, every parent render produced new closures
  // and defeated downstream React.memo bailouts.
  const handleIgnoreOne = useCallback(async (id: string) => {
    const ok = await confirm({
      title: "Ignore this discovered client?",
      body: "It will not appear in pending review unless reset.",
      confirmLabel: "Ignore",
      variant: "danger",
    });
    if (!ok) return;
    await ignore(id);
  }, [confirm, ignore]);

  const handleIgnoreMany = useCallback(async (ids: string[]) => {
    if (ids.length === 0) return;
    const ok = await confirm({
      title: `Ignore ${ids.length} discovered record${ids.length === 1 ? "" : "s"}?`,
      body: "Ignored candidates are filtered out of future scans until re-discovered.",
      confirmLabel: "Ignore",
      variant: "danger",
    });
    if (!ok) return;
    await ignoreMany(ids);
  }, [confirm, ignoreMany]);

  const handleAdoptMany = useCallback(async (ids: string[]) => {
    if (ids.length === 0) return;
    // `ids` is the flat list of raw discovered-records (one per node per
    // logical client). Count logical clients by how many unique groups
    // the selected ids belong to — that's what the operator sees as
    // rows in the table.
    const idSet = new Set(ids);
    const touchedGroups = groupDiscovered(discoveredClients).filter((g) =>
      g.ids.some((id) => idSet.has(id)),
    );
    const clients = touchedGroups.length || ids.length;
    const ok = await confirm({
      title: `Adopt ${clients} client${clients === 1 ? "" : "s"}?`,
      body:
        clients === 1
          ? "The client will be registered as managed on every node where it was discovered."
          : `Each of the ${clients} clients will be registered as managed on every node where it was discovered.`,
      confirmLabel: "Adopt",
      variant: "default",
    });
    if (!ok) return;
    await adoptMany(ids);
  }, [confirm, adoptMany, discoveredClients]);

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
