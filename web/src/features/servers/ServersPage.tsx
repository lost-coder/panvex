// P3-FE-01: recomposed locally from UI-kit primitives/components/compositions
// instead of importing the pre-built page from @lost-coder/panvex-ui/pages.
import { useMemo, useState } from "react";
import { NodeCard } from "@/features/servers/ui/NodeCard";
import { NodeSummaryCard } from "@/features/servers/ui/NodeSummaryCard";
import {
  BulkActionBar,
  Button,
  DataTable,
  PageHeader,
  StatusDot,
  TableView,
  cn,
  type BulkServerAction,
  type ServerListItem,
  type ServersPageProps,
  type ViewMode,
} from "@/ui";

function TrafficCell({ bytes }: { bytes: number }) {
  return (
    <span className="text-sm font-mono text-fg-muted">
      {Math.round(bytes / 1024 / 1024 / 1024)} GB
    </span>
  );
}

function DcMatrixCell({ dcs }: { dcs: ServerListItem["dcs"] }) {
  if (!dcs || dcs.length === 0) return <span className="text-xs text-fg-muted">N/A</span>;
  // Handoff-style "12 thin bars in a row" instead of a 6×2 dot grid. Each
  // bar is 4×14px so a full row fits in ~64px and the status distribution
  // across DCs reads as a single glance — green wall with occasional red
  // notches stands out more than a circular grid.
  return (
    <div className="flex items-center gap-[2px] w-fit">
      {dcs.slice(0, 12).map((dc, i) => (
        <div
          key={i}
          className={cn(
            "w-[4px] h-[14px] rounded-sm",
            dc.status === "error"
              ? "bg-status-error"
              : dc.status === "warn"
                ? "bg-status-warn"
                : "bg-status-ok/80",
          )}
          title={`DC ${dc.dc}: ${dc.rttMs ? dc.rttMs + "ms" : "offline"}`}
        />
      ))}
    </div>
  );
}

function ServerCardView({
  servers,
  onServerClick,
}: {
  servers: ServerListItem[];
  onServerClick?: ((id: string) => void) | undefined;
}) {
  return (
    <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
      {servers.map((s) => (
        <NodeSummaryCard
          key={s.id}
          name={s.name}
          status={s.status}
          connections={s.connections}
          trafficBytes={s.trafficBytes}
          cpuPct={s.cpuPct}
          memPct={s.memPct}
          dcs={s.dcs || []}
          onClick={() => onServerClick?.(s.id)}
        />
      ))}
    </div>
  );
}

interface ServerSelectionConfig {
  selected: Set<string>;
  onToggle: (id: string) => void;
  onToggleAll: () => void;
  allSelected: boolean;
  someSelected: boolean;
}

function ServerListView({
  servers,
  onServerClick,
  visibleColumns,
  selection,
}: {
  servers: ServerListItem[];
  onServerClick?: ((id: string) => void) | undefined;
  visibleColumns: Record<string, boolean>;
  selection?: ServerSelectionConfig | undefined;
}) {
  const allColumns = [
    ...(selection
      ? [
          {
            key: "select",
            header: (
              <input
                type="checkbox"
                aria-label="Select all servers on this page"
                checked={selection.allSelected}
                ref={(el) => {
                  if (el) el.indeterminate = selection.someSelected && !selection.allSelected;
                }}
                onChange={selection.onToggleAll}
                onClick={(e) => e.stopPropagation()}
                className="accent-accent size-4 cursor-pointer"
              />
            ) as unknown as string,
            render: (s: ServerListItem) => (
              <input
                type="checkbox"
                aria-label={`Select ${s.name}`}
                checked={selection.selected.has(s.id)}
                onChange={() => selection.onToggle(s.id)}
                onClick={(e) => e.stopPropagation()}
                className="accent-accent size-4 cursor-pointer"
              />
            ),
            className: "w-[36px] text-center",
          },
        ]
      : []),
    {
      key: "server",
      header: "Server",
      render: (s: ServerListItem) => (
        <div className="flex flex-col gap-0.5 min-w-0">
          <div className="flex items-center gap-2">
            <StatusDot status={s.status} />
            <span className="text-sm font-medium text-fg truncate">{s.name}</span>
          </div>
          {s.ip && <span className="pl-[14px] text-[10px] text-fg-muted font-mono">{s.ip}</span>}
        </div>
      ),
      sortable: true,
      className: "w-[30%]",
    },
    {
      key: "dcs",
      header: "DCs",
      render: (s: ServerListItem) => <DcMatrixCell dcs={s.dcs} />,
      // Wider to accommodate the 12-bar strip (4px bars + 2px gaps).
      className: "hidden xl:table-cell w-[92px]",
    },
    {
      key: "users",
      header: "Users",
      render: (s: ServerListItem) => (
        <div className="flex items-baseline gap-1 font-mono whitespace-nowrap justify-center">
          <span className="text-sm text-fg">
            {(s.usersOnline ?? s.connections).toLocaleString()}
          </span>
          <span className="text-xs text-fg-muted">
            /{(s.usersTotal ?? s.connections * 2).toLocaleString()}
          </span>
        </div>
      ),
      sortable: true,
      className: "hidden sm:table-cell text-center w-[110px]",
    },
    {
      key: "traffic",
      header: "Traffic",
      render: (s: ServerListItem) => (
        <div className="flex justify-center">
          <TrafficCell bytes={s.trafficBytes} />
        </div>
      ),
      sortable: true,
      className: "hidden md:table-cell text-center w-[80px]",
    },
    {
      key: "uptime",
      header: "Uptime",
      render: (s: ServerListItem) => {
        const days = Math.floor(s.uptimeSeconds / 86400);
        const hours = Math.floor((s.uptimeSeconds % 86400) / 3600);
        return (
          <div className="flex justify-center">
            <span className="text-xs font-mono text-fg-muted whitespace-nowrap">
              {days}d {hours}h
            </span>
          </div>
        );
      },
      sortable: true,
      className: "hidden lg:table-cell text-center w-[70px]",
    },
    {
      key: "load",
      header: "Load",
      render: (s: ServerListItem) => (
        <div className="flex flex-col gap-1">
          <div className="flex items-center gap-1.5 text-[10px] font-mono leading-none">
            <span className="w-7 text-fg-muted shrink-0">CPU</span>
            <div className="h-1.5 flex-1 bg-border rounded-full overflow-hidden">
              <div className="h-full bg-fg rounded-full" style={{ width: `${s.cpuPct}%` }} />
            </div>
            <span className="text-fg-muted w-7 text-right shrink-0">{s.cpuPct}%</span>
          </div>
          <div className="flex items-center gap-1.5 text-[10px] font-mono leading-none">
            <span className="w-7 text-fg-muted shrink-0">MEM</span>
            <div className="h-1.5 flex-1 bg-border rounded-full overflow-hidden">
              <div className="h-full bg-fg-muted rounded-full" style={{ width: `${s.memPct}%` }} />
            </div>
            <span className="text-fg-muted w-7 text-right shrink-0">{s.memPct}%</span>
          </div>
        </div>
      ),
      className: "hidden lg:table-cell w-[140px]",
    },
  ];

  const columns = allColumns.filter((c) => c.key === "server" || visibleColumns[c.key] !== false);

  return (
    <div className="bg-bg-card border border-border rounded-xl shadow-sm overflow-hidden">
      {/* Mobile: NodeCard list */}
      <div className="md:hidden flex flex-col gap-2 p-4 bg-bg">
        {servers.map((s) => (
          <NodeCard
            key={s.id}
            name={s.name}
            status={s.status}
            health={100}
            cpu={s.cpuPct}
            mem={s.memPct}
            clients={s.connections}
            region="Global"
            onClick={() => onServerClick?.(s.id)}
          />
        ))}
      </div>
      {/* Desktop: DataTable */}
      <div className="hidden md:block">
        <DataTable
          columns={columns}
          data={servers}
          keyExtractor={(s) => s.id}
          onRowClick={(s) => onServerClick?.(s.id)}
        />
      </div>
    </div>
  );
}

export function ServersPage({
  servers,
  viewMode,
  autoThreshold = 6,
  fleetGroups,
  onViewModeChange,
  onServerClick,
  onAddServer,
  onManageTokens,
  onBulkAction,
  bulkError,
  bulkPending,
}: ServersPageProps) {
  const [search, setSearch] = useState("");
  const [statusFilter, setStatusFilter] = useState("all");
  const [groupFilter, setGroupFilter] = useState("all");
  const [currentPage, setCurrentPage] = useState(1);
  const [columnVisibility, setColumnVisibility] = useState<Record<string, boolean>>({
    dcs: true,
    users: true,
    traffic: true,
    uptime: true,
    load: true,
  });
  // Multi-select state for bulk actions. `Set` keeps toggling O(1) and
  // survives re-renders via useState's ref stability.
  const [selected, setSelected] = useState<Set<string>>(() => new Set());
  const pageSize = 20;

  const effectiveMode: ViewMode = viewMode ?? (servers.length <= autoThreshold ? "cards" : "list");

  // Filtering
  const filtered = servers.filter((s) => {
    const matchSearch = !search || s.name.toLowerCase().includes(search.toLowerCase());
    const matchStatus = statusFilter === "all" || s.status === statusFilter;
    const matchGroup = groupFilter === "all" || s.fleetGroupId === groupFilter;
    return matchSearch && matchStatus && matchGroup;
  });

  // Counts are derived from the unfiltered fleet so the chips keep
  // showing the full distribution regardless of the active filter.
  // Displayed as " · N" suffix in each chip's label.
  const statusCounts = {
    all: servers.length,
    ok: servers.filter((s) => s.status === "ok").length,
    warn: servers.filter((s) => s.status === "warn").length,
    error: servers.filter((s) => s.status === "error").length,
  };


  const totalPages = Math.max(1, Math.ceil(filtered.length / pageSize));
  const paginated = filtered.slice((currentPage - 1) * pageSize, currentPage * pageSize);

  // Select-all toggles just the currently visible page — a fleet-wide
  // select-all would be dangerous for bulk destructive actions.
  const pageIds = useMemo(() => paginated.map((s) => s.id), [paginated]);
  const selectedOnPage = pageIds.filter((id) => selected.has(id));
  const allSelectedOnPage = pageIds.length > 0 && selectedOnPage.length === pageIds.length;
  const someSelectedOnPage = selectedOnPage.length > 0 && !allSelectedOnPage;

  const toggleOne = (id: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };
  const toggleAllOnPage = () => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (allSelectedOnPage) pageIds.forEach((id) => next.delete(id));
      else pageIds.forEach((id) => next.add(id));
      return next;
    });
  };
  const clearSelection = () => setSelected(new Set());

  const runBulk = async (action: BulkServerAction) => {
    if (!onBulkAction || selected.size === 0) return;
    const ids = Array.from(selected);
    await Promise.resolve(onBulkAction(action, ids));
    clearSelection();
  };

  return (
    <>
      <PageHeader
        title="Servers"
        subtitle={`${servers.length} active nodes`}
        trailing={
          onManageTokens || onAddServer ? (
            <div className="flex items-center gap-2">
              {onManageTokens && (
                <Button variant="ghost" size="sm" onClick={onManageTokens}>
                  Manage Tokens
                </Button>
              )}
              {onAddServer && (
                <Button size="sm" onClick={onAddServer}>
                  Add Server
                </Button>
              )}
            </div>
          ) : undefined
        }
      />
      <div className="px-4 md:px-8 pb-8 flex flex-col gap-5">
        <BulkActionBar
          count={selected.size}
          hint="run a bulk action or clear the selection"
          actions={[
            {
              id: "reload",
              label: "Reload runtime",
              variant: "ghost",
              disabled: !onBulkAction,
            },
            {
              id: "selfUpdate",
              label: "Self-update",
              variant: "ghost",
              disabled: !onBulkAction,
            },
          ]}
          onAction={(id) => runBulk(id as BulkServerAction)}
          onClear={clearSelection}
          pending={bulkPending}
          error={bulkError}
        />
        <TableView
          search={{
            value: search,
            onChange: (v) => {
              setSearch(v);
              setCurrentPage(1);
            },
            placeholder: "Search by name or IP...",
          }}
          filters={[
            {
              key: "status",
              value: statusFilter,
              onChange: (v) => {
                setStatusFilter(v);
                setCurrentPage(1);
              },
              variant: "chips" as const,
              options: [
                { value: "all", label: `All · ${statusCounts.all}` },
                { value: "ok", label: `Online · ${statusCounts.ok}`, tone: "ok" as const },
                { value: "warn", label: `Warning · ${statusCounts.warn}`, tone: "warn" as const },
                { value: "error", label: `Error · ${statusCounts.error}`, tone: "error" as const },
              ],
              placeholder: "Status",
            },
            {
              key: "group",
              value: groupFilter,
              onChange: (v) => {
                setGroupFilter(v);
                setCurrentPage(1);
              },
              options: [
                { value: "all", label: "All Groups" },
                ...(fleetGroups ?? []).map((g) => ({
                  value: g.id,
                  label: g.label ?? g.name ?? g.id,
                })),
              ],
              placeholder: "Group",
            },
          ]}
          viewMode={
            onViewModeChange ? { current: effectiveMode, onChange: onViewModeChange } : undefined
          }
          columns={{
            available: [
              { key: "dcs", label: "DC Matrix" },
              { key: "users", label: "Users" },
              { key: "traffic", label: "Traffic" },
              { key: "uptime", label: "Uptime" },
              { key: "load", label: "Load" },
            ],
            visibility: columnVisibility,
            onChange: (key, visible) =>
              setColumnVisibility((prev) => ({ ...prev, [key]: visible })),
          }}
          pagination={{
            page: currentPage,
            totalPages,
            totalItems: filtered.length,
            pageSize,
            onChange: setCurrentPage,
          }}
        >
          {/* Mobile: always list */}
          <div className="block md:hidden">
            <ServerListView
              servers={paginated}
              onServerClick={onServerClick}
              visibleColumns={columnVisibility}
            />
          </div>
          <div className="hidden md:block">
            {effectiveMode === "cards" ? (
              <ServerCardView servers={paginated} onServerClick={onServerClick} />
            ) : (
              <ServerListView
                servers={paginated}
                onServerClick={onServerClick}
                visibleColumns={columnVisibility}
                selection={{
                  selected,
                  onToggle: toggleOne,
                  onToggleAll: toggleAllOnPage,
                  allSelected: allSelectedOnPage,
                  someSelected: someSelectedOnPage,
                }}
              />
            )}
          </div>
        </TableView>
      </div>
    </>
  );
}
