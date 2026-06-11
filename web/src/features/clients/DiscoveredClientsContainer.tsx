import { useCallback, useState } from "react";
import { useTranslation } from "react-i18next";
import { DiscoveredClientsPage } from "./DiscoveredClientsPage";
import { useDiscoveredClients } from "./hooks/useDiscoveredClients";
import { useNavigate } from "@tanstack/react-router";
import { ErrorState } from "@/components/ErrorState";
import { SkeletonRows } from "@/ui";
import { useConfirm } from "@/app/providers/ConfirmProvider";
import { useUrlSearchState } from "@/shared/hooks/useUrlSearchState";
import { groupDiscovered } from "./lib/groupDiscovered";

export function DiscoveredClientsContainer() {
  const { t } = useTranslation("clients");
  // U-05: pause the live poll while the operator has a selection active so
  // the list does not reflow under their finger mid-triage.
  const [selectionActive, setSelectionActive] = useState(false);
  const { discoveredClients, isLoading, error, refetch, adopt, ignore, adoptMany, ignoreMany, rescan, isAdopting, isIgnoring, isRescanning } =
    useDiscoveredClients({ pausePolling: selectionActive });
  const navigate = useNavigate();
  const confirm = useConfirm();

  // P2-UX-05: persist a coarse "which tab am I on" URL param so operators
  // can share a link mid-triage. The DiscoveredClientsPage owns the
  // actual filter UI internally; tying it into the URL is left to a
  // follow-up ticket (see deferred notes in commit body).
  useUrlSearchState("filter", "");

  // Q5.U-Q-24 fix: hooks MUST run on every render. The previous version
  // declared useCallback after the early returns above, which violates
  // the rules-of-hooks. Move them up.
  const handleIgnoreOne = useCallback(async (id: string) => {
    const ok = await confirm({
      title: t("discovered.confirm.ignoreOneTitle"),
      body: t("discovered.confirm.ignoreOneBody"),
      confirmLabel: t("discovered.confirm.ignoreConfirm"),
      variant: "danger",
    });
    if (!ok) return;
    await ignore(id);
  }, [confirm, ignore, t]);

  const handleIgnoreMany = useCallback(async (ids: string[]) => {
    if (ids.length === 0) return;
    const ok = await confirm({
      title: t("discovered.confirm.ignoreManyTitle", { count: ids.length }),
      body: t("discovered.confirm.ignoreManyBody"),
      confirmLabel: t("discovered.confirm.ignoreConfirm"),
      variant: "danger",
    });
    if (!ok) return;
    await ignoreMany(ids);
  }, [confirm, ignoreMany, t]);

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
      title: t("discovered.confirm.adoptManyTitle", { count: clients }),
      body:
        clients === 1
          ? t("discovered.confirm.adoptOneBody")
          : t("discovered.confirm.adoptManyBody", { count: clients }),
      confirmLabel: t("discovered.confirm.adoptConfirm"),
      variant: "default",
    });
    if (!ok) return;
    await adoptMany(ids);
  }, [confirm, adoptMany, discoveredClients, t]);

  if (isLoading) {
    return (
      <div className="px-4 md:px-8 py-8">
        <SkeletonRows count={5} />
      </div>
    );
  }

  if (error) {
    return <ErrorState description={error.message} onRetry={() => void refetch()} />;
  }

  return (
    <DiscoveredClientsPage
      clients={discoveredClients}
      onAdopt={(id) => adopt(id)}
      onIgnore={handleIgnoreOne}
      onAdoptMany={handleAdoptMany}
      onIgnoreMany={handleIgnoreMany}
      onBack={() => navigate({ to: "/clients" })}
      onRescan={() => rescan()}
      onSelectionActiveChange={setSelectionActive}
      busy={isAdopting || isIgnoring}
      rescanning={isRescanning}
    />
  );
}
