// Phase-7 redesign of /clients/discovered. Front-end dedup by
// clientName as a stop-gap for backend-followup #6 — a single logical
// client shows up once in the list with `discoveredOn: node[]` instead
// of one row per node.
import { useMemo, useState } from "react";

import {
  Badge,
  Button,
  DataTable,
  EmptyState,
  PageHeader,
  StatusDot,
  TableView,
  cn,
  formatBytes,
  formatQuota,
} from "@/ui";
import type { DiscoveredClientsPageProps } from "@/shared/api/types-pages/pages";
import { groupDiscovered, type DiscoveredGroup } from "@/features/clients/lib/groupDiscovered";

// ─── Small helpers ───────────────────────────────────────────────────

function StatusPill({ status }: { status: DiscoveredGroup["status"] }) {
  if (status === "adopted") return <Badge variant="ok">Adopted</Badge>;
  if (status === "ignored") return <Badge variant="default">Ignored</Badge>;
  if (status === "mixed") return <Badge variant="warn">Mixed</Badge>;
  return <Badge variant="warn">Pending</Badge>;
}

function buildColumns(opts: {
  selection?: {
    selected: Set<string>;
    onToggle: (key: string) => void;
    onToggleAll: () => void;
    allSelected: boolean;
    someSelected: boolean;
  } | undefined;
  onAdopt?: ((ids: string[]) => void) | undefined;
  onIgnore?: ((ids: string[]) => void) | undefined;
  busy?: boolean | undefined;
  withActions: boolean;
}) {
  const { selection, onAdopt, onIgnore, busy, withActions } = opts;
  const cols: Array<{
    key: string;
    header: string;
    render: (row: DiscoveredGroup) => React.ReactNode;
    className?: string;
  }> = [];

  if (selection) {
    cols.push({
      key: "select",
      // DataTable expects header:string; for a checkbox we smuggle a
      // JSX node through the string slot. Other tables in the codebase
      // use the same pattern.
      header: (
        <input
          type="checkbox"
          aria-label="Select all on this page"
          checked={selection.allSelected}
          ref={(el) => {
            if (el) el.indeterminate = selection.someSelected && !selection.allSelected;
          }}
          onChange={selection.onToggleAll}
          onClick={(e) => e.stopPropagation()}
          className="accent-accent size-4 cursor-pointer"
        />
      ) as unknown as string,
      render: (row) => (
        <input
          type="checkbox"
          aria-label={`Select ${row.clientName}`}
          checked={selection.selected.has(row.key)}
          onChange={() => selection.onToggle(row.key)}
          onClick={(e) => e.stopPropagation()}
          className="accent-accent size-4 cursor-pointer"
        />
      ),
      className: "w-[36px] text-center",
    });
  }

  cols.push(
    {
      key: "client",
      header: "Client",
      render: (row) => (
        <div className="flex items-center gap-2 min-w-0">
          <StatusDot
            status={
              row.status === "adopted"
                ? "ok"
                : row.status === "ignored"
                  ? "warn"
                  : row.hasConflict
                    ? "error"
                    : "warn"
            }
          />
          <span className="font-medium text-fg truncate">{row.clientName}</span>
          {row.hasNameConflict ? (
            <Badge variant="error">name conflict</Badge>
          ) : row.hasConflict ? (
            <Badge variant="warn">conflict</Badge>
          ) : null}
        </div>
      ),
      className: "w-[26%]",
    },
    {
      key: "nodes",
      header: "Discovered on",
      render: (row) => (
        <div className="flex flex-wrap gap-1 min-w-0">
          {row.discoveredOn.length === 0 ? (
            <span className="text-xs text-fg-muted">—</span>
          ) : (
            row.discoveredOn.map((n) => (
              <span
                key={n}
                className="font-mono text-[10px] text-fg-muted px-1.5 py-0.5 rounded-xs border border-divider bg-bg"
              >
                {n}
              </span>
            ))
          )}
          {row.ids.length > 1 && (
            <span className="text-[10px] font-mono text-fg-muted px-1">×{row.ids.length}</span>
          )}
        </div>
      ),
      className: "w-[30%]",
    },
    {
      key: "usage",
      header: "Usage",
      render: (row) => (
        <div className="flex flex-col font-mono text-[11px]">
          <span className="text-fg tabular-nums">
            {row.currentConnections} conns · {row.activeUniqueIps} IPs
          </span>
          <span className="text-fg-muted tabular-nums">
            {formatBytes(row.totalOctets)}
            {row.dataQuotaBytes > 0 ? ` / ${formatQuota(row.dataQuotaBytes)}` : ""}
          </span>
        </div>
      ),
      className: "hidden md:table-cell w-[170px]",
    },
    {
      key: "discovered",
      header: "Discovered at",
      render: (row) => (
        <span className="text-[11px] font-mono text-fg-muted tabular-nums">
          {Number.isFinite(row.discoveredAtUnix) && row.discoveredAtUnix > 0
            ? new Date(row.discoveredAtUnix * 1000).toLocaleString()
            : "—"}
        </span>
      ),
      className: "hidden lg:table-cell w-[170px]",
    },
    {
      key: "status",
      header: "Status",
      render: (row) => <StatusPill status={row.status} />,
      className: "w-[110px]",
    },
  );

  if (withActions) {
    cols.push({
      key: "actions",
      header: "",
      render: (row) => (
        <div className="flex items-center gap-2 justify-end">
          <Button
            size="sm"
            disabled={busy}
            onClick={(e) => {
              e.stopPropagation();
              onAdopt?.(row.ids);
            }}
          >
            Adopt
          </Button>
          <Button
            size="sm"
            variant="outline"
            disabled={busy}
            onClick={(e) => {
              e.stopPropagation();
              onIgnore?.(row.ids);
            }}
          >
            Ignore
          </Button>
        </div>
      ),
      className: "w-[180px]",
    });
  }

  return cols;
}

function MobileRow({
  row,
  selected,
  onToggleSelect,
  onAdopt,
  onIgnore,
  busy,
}: {
  row: DiscoveredGroup;
  selected: boolean;
  onToggleSelect: (key: string) => void;
  onAdopt?: ((ids: string[]) => void) | undefined;
  onIgnore?: ((ids: string[]) => void) | undefined;
  busy?: boolean | undefined;
}) {
  const interactive = row.status === "pending_review";
  return (
    <div className="flex flex-col gap-2 px-4 py-3 border-b border-divider">
      <div className="flex items-center gap-3">
        {interactive && (
          <input
            type="checkbox"
            aria-label={`Select ${row.clientName}`}
            checked={selected}
            onChange={() => onToggleSelect(row.key)}
            className="accent-accent size-4 cursor-pointer"
          />
        )}
        <span className="font-medium text-fg truncate flex-1">{row.clientName}</span>
        <StatusPill status={row.status} />
      </div>
      <div className="flex flex-wrap gap-1 pl-7">
        {row.discoveredOn.map((n) => (
          <span
            key={n}
            className="font-mono text-[10px] text-fg-muted px-1.5 py-0.5 rounded-xs border border-divider bg-bg"
          >
            {n}
          </span>
        ))}
      </div>
      <div className="flex items-center justify-between pl-7 text-[11px] font-mono text-fg-muted">
        <span>
          {row.currentConnections} conns · {row.activeUniqueIps} IPs · {formatBytes(row.totalOctets)}
        </span>
        {Number.isFinite(row.discoveredAtUnix) && row.discoveredAtUnix > 0 && (
          <span>{new Date(row.discoveredAtUnix * 1000).toLocaleString()}</span>
        )}
      </div>
      {interactive && (
        <div className="flex gap-2 pl-7">
          <Button size="sm" disabled={busy} onClick={() => onAdopt?.(row.ids)}>
            Adopt
          </Button>
          <Button size="sm" variant="outline" disabled={busy} onClick={() => onIgnore?.(row.ids)}>
            Ignore
          </Button>
        </div>
      )}
    </div>
  );
}

// ─── Pulse cell ──────────────────────────────────────────────────────

function PulseCell({
  i,
  label,
  value,
  tone,
}: {
  i: number;
  label: string;
  value: number;
  tone?: "default" | "ok" | "warn" | "error";
}) {
  const isSecondCol = i % 2 === 1;
  const isSecondRow = i >= 2;
  const toneClass: Record<NonNullable<typeof tone>, string> = {
    default: "text-fg",
    ok: "text-status-ok",
    warn: "text-status-warn",
    error: "text-status-error",
  };
  return (
    <div
      className={cn(
        "min-w-0 p-4",
        isSecondCol && "border-l border-divider",
        isSecondRow && "border-t border-divider md:border-t-0",
        i > 0 && "md:border-l md:border-divider",
      )}
    >
      <div className="flex flex-col gap-1 min-w-0">
        <span className="text-[10px] font-mono uppercase tracking-wider text-fg-muted">{label}</span>
        <span
          className={cn(
            "text-2xl font-mono font-semibold leading-none tracking-tight tabular-nums",
            toneClass[tone ?? "default"],
          )}
        >
          {value.toLocaleString()}
        </span>
      </div>
    </div>
  );
}

// ─── Main page ───────────────────────────────────────────────────────

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

  const pendingColumns = buildColumns({
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
  const reviewedColumns = buildColumns({ busy, withActions: false });

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
          <PulseCell i={0} label="Pending" value={counts.pending} tone="warn" />
          <PulseCell i={1} label="Adopted" value={counts.adopted} tone="ok" />
          <PulseCell i={2} label="Ignored" value={counts.ignored} tone="default" />
          <PulseCell
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
                      <MobileRow
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
                      <MobileRow
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
