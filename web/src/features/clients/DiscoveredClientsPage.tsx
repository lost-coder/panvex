// Phase-7 redesign of /clients/discovered. Front-end dedup by
// clientName as a stop-gap for backend-followup #6 — a single logical
// client shows up once in the list with `discoveredOn: node[]` instead
// of one row per node.
//
// R-Q-08: pulse strip, filter spec, pending/reviewed sections, the
// column factory, and mobile row all live in `./components/`.
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";

import { Button, EmptyState, PageHeader, TableView } from "@/ui";
import type { DiscoveredClientsPageProps } from "@/shared/api/types-pages/pages";
import {
  groupDiscovered,
  type DiscoveredGroup,
} from "@/features/clients/lib/groupDiscovered";

import { buildDiscoveredColumns } from "./components/DiscoveredColumns";
import {
  DiscoveredPulseStrip,
  buildDiscoveredFilters,
  type DiscoveredCounts,
} from "./components/DiscoveredPulseStrip";
import {
  DiscoveredPendingSection,
  DiscoveredReviewedSection,
} from "./components/DiscoveredSection";

function buildCounts(groups: DiscoveredGroup[]): DiscoveredCounts {
  let pending = 0;
  let adopted = 0;
  let ignored = 0;
  let conflicts = 0;
  for (const g of groups) {
    if (g.status === "pending_review") pending++;
    else if (g.status === "adopted") adopted++;
    else if (g.status === "ignored") ignored++;
    if (g.hasConflict) conflicts++;
  }
  return { all: groups.length, pending, adopted, ignored, conflicts };
}

export function DiscoveredClientsPage({
  clients,
  onAdopt,
  onIgnore,
  onAdoptMany,
  onIgnoreMany,
  onBack,
  onRescan,
  onSelectionActiveChange,
  busy,
  rescanning,
}: Readonly<DiscoveredClientsPageProps>) {
  const { t } = useTranslation("clients");
  const groups = useMemo(() => groupDiscovered(clients), [clients]);
  const counts = useMemo(() => buildCounts(groups), [groups]);

  const [search, setSearch] = useState("");
  const [statusFilter, setStatusFilter] = useState("all");
  const [conflictFilter, setConflictFilter] = useState("all");
  const [selected, setSelected] = useState<Set<string>>(new Set());

  // U-05: tell the container whether a selection is active so it can
  // pause/resume the live poll (prevents the list reflowing under a tap).
  useEffect(() => {
    onSelectionActiveChange?.(selected.size > 0);
  }, [selected, onSelectionActiveChange]);

  const matches = (g: Readonly<DiscoveredGroup>) => {
    if (search) {
      const q = search.toLowerCase();
      if (
        !g.clientName.toLowerCase().includes(q) &&
        !g.discoveredOn.some((n) => n.toLowerCase().includes(q))
      ) {
        return false;
      }
    }
    if (statusFilter === "pending" && g.status !== "pending_review") return false;
    if (statusFilter === "adopted" && g.status !== "adopted") return false;
    if (statusFilter === "ignored" && g.status !== "ignored") return false;
    if (conflictFilter === "only" && !g.hasConflict) return false;
    return true;
  };
  const filtered = groups.filter(matches);
  const pendingList = filtered.filter((g) => g.status === "pending_review");
  const reviewedList = filtered.filter((g) => g.status !== "pending_review");

  const pendingKeys = pendingList.map((g) => g.key);
  const allPendingSelected =
    pendingKeys.length > 0 && pendingKeys.every((k) => selected.has(k));
  const somePendingSelected =
    pendingKeys.some((k) => selected.has(k)) && !allPendingSelected;
  const toggleOne = (key: string) =>
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  const toggleAll = () =>
    setSelected((prev) => {
      const next = new Set(prev);
      if (allPendingSelected) pendingKeys.forEach((k) => next.delete(k));
      else pendingKeys.forEach((k) => next.add(k));
      return next;
    });
  const clearSelection = () => setSelected(new Set());

  const selectedIds = pendingList.filter((g) => selected.has(g.key)).flatMap((g) => g.ids);

  const runAdopt = (ids: string[]) => {
    if (ids.length === 0) return;
    if (ids.length === 1) onAdopt?.(ids[0]!);
    else onAdoptMany?.(ids);
  };
  const runIgnore = (ids: string[]) => {
    if (ids.length === 0) return;
    if (ids.length === 1) onIgnore?.(ids[0]!);
    else onIgnoreMany?.(ids);
  };
  const runBulkAdopt = () => {
    if (selectedIds.length === 0) return;
    onAdoptMany?.(selectedIds);
    clearSelection();
  };
  const runBulkIgnore = () => {
    if (selectedIds.length === 0) return;
    onIgnoreMany?.(selectedIds);
    clearSelection();
  };

  const pendingColumns = buildDiscoveredColumns({
    selection: {
      selected,
      onToggle: toggleOne,
      onToggleAll: toggleAll,
      allSelected: allPendingSelected,
      someSelected: somePendingSelected,
    },
    onAdopt: runAdopt,
    onIgnore: runIgnore,
    busy,
    withActions: true,
    t,
  });
  const reviewedColumns = buildDiscoveredColumns({ busy, withActions: false, t });

  return (
    <>
      <PageHeader
        title={t("discovered.title")}
        subtitle={t("discovered.subtitle", {
          pending: counts.pending,
          adopted: counts.adopted,
          ignored: counts.ignored,
        })}
        trailing={
          onBack ? (
            <Button size="sm" variant="outline" onClick={onBack}>
              {t("discovered.back")}
            </Button>
          ) : undefined
        }
      />
      <div className="px-4 md:px-8 pb-8 flex flex-col gap-5">
        <DiscoveredPulseStrip counts={counts} />

        <TableView
          search={{
            value: search,
            onChange: setSearch,
            placeholder: t("discovered.searchPlaceholder"),
          }}
          filters={buildDiscoveredFilters({
            status: { value: statusFilter, onChange: setStatusFilter },
            conflicts: { value: conflictFilter, onChange: setConflictFilter },
            counts,
            t,
          })}
        >
          <div className="flex flex-col gap-6">
            <DiscoveredPendingSection
              rows={pendingList}
              columns={pendingColumns}
              selected={selected}
              selectedRecordCount={selectedIds.length}
              onToggleSelect={toggleOne}
              onAdopt={runAdopt}
              onIgnore={runIgnore}
              onClearSelection={clearSelection}
              onBulkAdopt={runBulkAdopt}
              onBulkIgnore={runBulkIgnore}
              onRescan={onRescan}
              busy={busy}
              rescanning={rescanning}
            />
            {filtered.length === 0 ? (
              <EmptyState
                title={t("discovered.empty.title")}
                description={t("discovered.empty.description")}
              />
            ) : (
              <DiscoveredReviewedSection
                rows={reviewedList}
                columns={reviewedColumns}
                busy={busy}
              />
            )}
          </div>
        </TableView>
      </div>
    </>
  );
}
