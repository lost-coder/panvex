// Phase-7 redesign of /clients/discovered. Front-end dedup by
// clientName as a stop-gap for backend-followup #6 — a single logical
// client shows up once in the list with `discoveredOn: node[]` instead
// of one row per node.
//
// R-Q-08: column factory, mobile row, pulse cell, and the StatusPill
// helper live in `./components/` so this file stays focused on filter
// state, selection, and bulk-action dispatch.
import { useMemo, useState } from "react";

import { Button, DataTable, EmptyState, PageHeader, TableView } from "@/ui";
import type { DiscoveredClientsPageProps } from "@/shared/api/types-pages/pages";
import {
  groupDiscovered,
  type DiscoveredGroup,
} from "@/features/clients/lib/groupDiscovered";

import { buildDiscoveredColumns } from "./components/DiscoveredColumns";
import {
  DiscoveredMobileRow,
  DiscoveredPulseCell,
} from "./components/DiscoveredMobileRow";

export function DiscoveredClientsPage({
  clients,
  onAdopt,
  onIgnore,
  onAdoptMany,
  onIgnoreMany,
  onBack,
  busy,
}: DiscoveredClientsPageProps) {
  const groups = useMemo(() => groupDiscovered(clients), [clients]);
  const counts = useMemo(() => {
    let pending = 0,
      adopted = 0,
      ignored = 0,
      conflicts = 0;
    for (const g of groups) {
      if (g.status === "pending_review") pending++;
      else if (g.status === "adopted") adopted++;
      else if (g.status === "ignored") ignored++;
      if (g.hasConflict) conflicts++;
    }
    return { all: groups.length, pending, adopted, ignored, conflicts };
  }, [groups]);

  const [search, setSearch] = useState("");
  const [statusFilter, setStatusFilter] = useState("all");
  const [conflictFilter, setConflictFilter] = useState("all");
  const [selected, setSelected] = useState<Set<string>>(new Set());

  const matches = (g: DiscoveredGroup) => {
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

  const selectedIds = pendingList
    .filter((g) => selected.has(g.key))
    .flatMap((g) => g.ids);

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
  });
  const reviewedColumns = buildDiscoveredColumns({ busy, withActions: false });

  return (
    <>
      <PageHeader
        title="Discovered clients"
        subtitle={`${counts.pending} pending · ${counts.adopted} adopted · ${counts.ignored} ignored`}
        trailing={
          onBack ? (
            <Button size="sm" variant="outline" onClick={onBack}>
              Back to Clients
            </Button>
          ) : undefined
        }
      />
      <div className="px-4 md:px-8 pb-8 flex flex-col gap-5">
        <section className="rounded-xs bg-bg-card border border-border grid grid-cols-2 md:grid-cols-4">
          <DiscoveredPulseCell i={0} label="Pending" value={counts.pending} tone="warn" />
          <DiscoveredPulseCell i={1} label="Adopted" value={counts.adopted} tone="ok" />
          <DiscoveredPulseCell i={2} label="Ignored" value={counts.ignored} tone="default" />
          <DiscoveredPulseCell
            i={3}
            label="Conflicts"
            value={counts.conflicts}
            tone={counts.conflicts > 0 ? "error" : "default"}
          />
        </section>

        <TableView
          search={{
            value: search,
            onChange: setSearch,
            placeholder: "Search client or node…",
          }}
          filters={[
            {
              key: "status",
              value: statusFilter,
              onChange: setStatusFilter,
              variant: "chips",
              options: [
                { value: "all", label: `All · ${counts.all}` },
                { value: "pending", label: `Pending · ${counts.pending}`, tone: "warn" as const },
                { value: "adopted", label: `Adopted · ${counts.adopted}`, tone: "ok" as const },
                { value: "ignored", label: `Ignored · ${counts.ignored}` },
              ],
              placeholder: "Status",
            },
            {
              key: "conflicts",
              value: conflictFilter,
              onChange: setConflictFilter,
              variant: "chips",
              options: [
                { value: "all", label: "All" },
                {
                  value: "only",
                  label: `Conflicts · ${counts.conflicts}`,
                  tone: "error" as const,
                },
              ],
              placeholder: "Conflicts",
            },
          ]}
        >
          {filtered.length === 0 ? (
            <EmptyState
              title="No discovered clients"
              description="Agents report users that aren't managed by the panel. They'll show up here for review."
            />
          ) : (
            <div className="flex flex-col gap-6">
              {pendingList.length > 0 && (
                <section className="flex flex-col gap-3">
                  <div className="flex items-center justify-between gap-3 flex-wrap">
                    <h3 className="text-sm font-semibold text-fg">
                      Pending ({pendingList.length})
                    </h3>
                    {selected.size > 0 && (
                      <div className="flex items-center gap-2 rounded-xs bg-bg-card border border-accent/40 px-3 py-1.5">
                        <span className="text-xs font-mono text-fg">
                          {selected.size} selected · {selectedIds.length} records
                        </span>
                        <Button size="sm" disabled={busy} onClick={runBulkAdopt}>
                          Adopt
                        </Button>
                        <Button size="sm" variant="outline" disabled={busy} onClick={runBulkIgnore}>
                          Ignore
                        </Button>
                        <Button size="sm" variant="ghost" onClick={clearSelection}>
                          Clear
                        </Button>
                      </div>
                    )}
                  </div>
                  <div className="md:hidden rounded-xs bg-bg-card border border-border overflow-hidden">
                    {pendingList.map((g) => (
                      <DiscoveredMobileRow
                        key={g.key}
                        row={g}
                        selected={selected.has(g.key)}
                        onToggleSelect={toggleOne}
                        onAdopt={runAdopt}
                        onIgnore={runIgnore}
                        busy={busy}
                      />
                    ))}
                  </div>
                  <div className="hidden md:block rounded-xs bg-bg-card border border-border overflow-hidden">
                    <DataTable
                      columns={pendingColumns}
                      data={pendingList}
                      keyExtractor={(row: DiscoveredGroup) => row.key}
                    />
                  </div>
                </section>
              )}

              {reviewedList.length > 0 && (
                <section className="flex flex-col gap-3">
                  <h3 className="text-sm font-semibold text-fg-muted">
                    Previously reviewed ({reviewedList.length})
                  </h3>
                  <div className="md:hidden rounded-xs bg-bg-card border border-border overflow-hidden">
                    {reviewedList.map((g) => (
                      <DiscoveredMobileRow
                        key={g.key}
                        row={g}
                        selected={false}
                        onToggleSelect={() => {}}
                        busy={busy}
                      />
                    ))}
                  </div>
                  <div className="hidden md:block rounded-xs bg-bg-card border border-border overflow-hidden">
                    <DataTable
                      columns={reviewedColumns}
                      data={reviewedList}
                      keyExtractor={(row: DiscoveredGroup) => row.key}
                    />
                  </div>
                </section>
              )}
            </div>
          )}
        </TableView>
      </div>
    </>
  );
}
