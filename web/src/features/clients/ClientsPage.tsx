// Phase-7 redesign: compact columns, status counts in chip labels, bulk
// actions toolbar (enable / disable / delete) with multi-select.
import { useMemo, useState } from "react";

import { ClientFormSheet } from "@/features/clients/ClientFormSheet";
import { DiscoveredClientsBanner } from "@/features/clients/DiscoveredClientsBanner";
import { useNowSec } from "@/shared/hooks/useNowSec";
import {
  Badge,
  BulkActionBar,
  Button,
  DataTable,
  MonoValue,
  PageHeader,
  PulseRow,
  Sheet,
  SheetBody,
  SheetContent,
  StatusDot,
  TableView,
  cn,
  formatBytes,
  formatExpiry,
  formatQuota,
  type BulkClientAction,
  type ClientFormData,
  type ClientListItem,
  type ClientsPageProps,
  type PulseTick,
  type ViewMode,
} from "@/ui";

// ─── Helpers ─────────────────────────────────────────────────────────

function isExpired(expirationRfc3339: string, nowMs: number): boolean {
  if (!expirationRfc3339) return false;
  const t = Date.parse(expirationRfc3339);
  return Number.isFinite(t) && t < nowMs;
}

function effectiveStatus(
  c: ClientListItem,
  nowMs: number,
): "active" | "disabled" | "expired" {
  if (isExpired(c.expirationRfc3339, nowMs)) return "expired";
  return c.enabled ? "active" : "disabled";
}

function StatusBadge({ status }: { status: "active" | "disabled" | "expired" }) {
  const map = {
    active: { label: "Active", variant: "ok" as const },
    disabled: { label: "Disabled", variant: "default" as const },
    expired: { label: "Expired", variant: "error" as const },
  };
  const { label, variant } = map[status];
  return <Badge variant={variant}>{label}</Badge>;
}

function TrafficCell({ used, quota }: { used: number; quota: number }) {
  // No quota → just show used bytes; with a quota render a slim
  // progress bar + "used / quota" so operators see headroom at a glance.
  if (!quota) {
    return <MonoValue className="text-fg">{formatBytes(used)}</MonoValue>;
  }
  const pct = Math.min(100, (used / quota) * 100);
  const tone =
    pct >= 100 ? "bg-status-error" : pct >= 80 ? "bg-status-warn" : "bg-status-ok";
  return (
    <div className="flex flex-col gap-1 min-w-[120px]">
      <span className="text-[11px] font-mono text-fg tabular-nums">
        {formatBytes(used)}
        <span className="text-fg-muted"> / {formatQuota(quota)}</span>
      </span>
      <div className="h-1 w-full rounded-full bg-border overflow-hidden">
        <div className={cn("h-full rounded-full", tone)} style={{ width: `${pct}%` }} />
      </div>
    </div>
  );
}

function ExpiryCell({ rfc, nowSec }: { rfc: string; nowSec: number }) {
  if (!rfc) return <span className="text-[11px] font-mono text-fg-muted">Never</span>;
  const t = Date.parse(rfc);
  if (!Number.isFinite(t)) return <span className="text-[11px] font-mono text-fg-muted">—</span>;
  // `nowSec` snapshot is threaded through props so this cell stays pure at
  // render time (react-hooks/purity lint rule). Callers use `useNowSec()` to
  // keep the snapshot fresh.
  const days = Math.floor((t / 1000 - nowSec) / 86_400);
  const tone =
    days < 0 ? "text-status-error" : days < 7 ? "text-status-warn" : "text-fg-muted";
  const subtitle = days < 0 ? `${Math.abs(days)}d ago` : days === 0 ? "today" : `in ${days}d`;
  return (
    <div className="flex flex-col">
      <span className="text-[11px] font-mono text-fg tabular-nums">{formatExpiry(rfc)}</span>
      <span className={cn("text-[10px] font-mono", tone)}>{subtitle}</span>
    </div>
  );
}

// ─── Multi-select column factory ─────────────────────────────────────

interface ClientSelectionConfig {
  selected: Set<string>;
  onToggle: (id: string) => void;
  onToggleAll: () => void;
  allSelected: boolean;
  someSelected: boolean;
}

function buildColumns(nowSec: number, selection?: ClientSelectionConfig) {
  return [
    ...(selection
      ? [
          {
            key: "select",
            header: (
              <input
                type="checkbox"
                aria-label="Select all clients on this page"
                checked={selection.allSelected}
                ref={(el) => {
                  if (el) el.indeterminate = selection.someSelected && !selection.allSelected;
                }}
                onChange={selection.onToggleAll}
                onClick={(e) => e.stopPropagation()}
                className="accent-accent size-4 cursor-pointer"
              />
            ) as unknown as string,
            render: (c: ClientListItem) => (
              <input
                type="checkbox"
                aria-label={`Select ${c.name}`}
                checked={selection.selected.has(c.id)}
                onChange={() => selection.onToggle(c.id)}
                onClick={(e) => e.stopPropagation()}
                className="accent-accent size-4 cursor-pointer"
              />
            ),
            className: "w-[36px] text-center",
          },
        ]
      : []),
    {
      key: "client",
      header: "Client",
      render: (c: ClientListItem) => (
        <div className="flex items-center gap-2 min-w-0">
          <StatusDot status={c.enabled ? "ok" : "error"} />
          <span className="font-medium text-fg truncate">{c.name}</span>
        </div>
      ),
      className: "w-[28%]",
    },
    {
      key: "status",
      header: "Status",
      render: (c: ClientListItem) => (
        <StatusBadge status={effectiveStatus(c, nowSec * 1000)} />
      ),
      className: "w-[120px]",
    },
    {
      key: "usage",
      header: "Usage",
      render: (c: ClientListItem) => (
        <div className="flex flex-col font-mono text-[11px]">
          <span className="text-fg tabular-nums">{c.activeTcpConns} conns</span>
          <span className="text-fg-muted tabular-nums">{c.uniqueIpsUsed} IPs</span>
        </div>
      ),
      className: "hidden md:table-cell w-[110px]",
    },
    {
      key: "traffic",
      header: "Traffic",
      render: (c: ClientListItem) => (
        <TrafficCell used={c.trafficUsedBytes} quota={c.dataQuotaBytes} />
      ),
      className: "hidden lg:table-cell w-[180px]",
    },
    {
      key: "expires",
      header: "Expires",
      render: (c: ClientListItem) => (
        <ExpiryCell rfc={c.expirationRfc3339} nowSec={nowSec} />
      ),
      className: "hidden md:table-cell w-[120px]",
    },
    {
      key: "nodes",
      header: "Nodes",
      render: (c: ClientListItem) => (
        <MonoValue className="text-fg-muted">{c.assignedNodesCount}</MonoValue>
      ),
      className: "hidden xl:table-cell w-[80px] text-center",
    },
  ];
}

// ─── Mobile compact row ──────────────────────────────────────────────

function ClientListRow({
  client,
  onClick,
  selectable,
  selected,
  onToggleSelect,
  nowMs,
}: {
  client: ClientListItem;
  onClick?: () => void;
  selectable?: boolean;
  selected?: boolean;
  onToggleSelect?: (id: string) => void;
  nowMs: number;
}) {
  const status = effectiveStatus(client, nowMs);
  return (
    <div
      onClick={onClick}
      className="flex items-center gap-3 px-4 py-3 border-b border-divider hover:bg-bg-hover transition-colors cursor-pointer"
    >
      {selectable && (
        <input
          type="checkbox"
          aria-label={`Select ${client.name}`}
          checked={!!selected}
          onChange={() => onToggleSelect?.(client.id)}
          onClick={(e) => e.stopPropagation()}
          className="accent-accent size-4 cursor-pointer"
        />
      )}
      <StatusDot status={client.enabled ? "ok" : "error"} />
      <div className="flex flex-col min-w-0 flex-1">
        <span className="font-medium text-fg truncate">{client.name}</span>
        <span className="text-[11px] font-mono text-fg-muted tabular-nums">
          {client.activeTcpConns} conns · {formatBytes(client.trafficUsedBytes)}
        </span>
      </div>
      <StatusBadge status={status} />
    </div>
  );
}

// ─── Main page ───────────────────────────────────────────────────────

const emptyFormData: ClientFormData = {
  name: "",
  userAdTag: "",
  expirationRfc3339: "",
  maxTcpConns: 0,
  maxUniqueIps: 0,
  dataQuotaBytes: 0,
  fleetGroupIds: [],
  agentIds: [],
};

export function ClientsPage({
  clients,
  viewMode,
  autoThreshold = 6,
  onViewModeChange,
  onClientClick,
  onCreate,
  createLoading,
  createError,
  fleetGroups,
  agents,
  pendingDiscoveredCount,
  onDiscoveredClick,
  onBulkAction,
  bulkError,
  bulkPending,
}: ClientsPageProps) {
  const [search, setSearch] = useState("");
  const [statusFilter, setStatusFilter] = useState("all");
  const [currentPage, setCurrentPage] = useState(1);
  const [createOpen, setCreateOpen] = useState(false);
  const [createData, setCreateData] = useState<ClientFormData>({ ...emptyFormData });
  const [selected, setSelected] = useState<Set<string>>(() => new Set());
  const pageSize = 20;
  // Auto-refreshing "now" — lifted out of the render path so `effectiveStatus`
  // and `ExpiryCell` stay pure (react-hooks/purity).
  const nowSec = useNowSec();
  const nowMs = nowSec * 1000;

  const effectiveMode: ViewMode = viewMode ?? (clients.length <= autoThreshold ? "cards" : "list");

  // Counts derived from the unfiltered list so chip labels keep the
  // full distribution regardless of the active filter. Also powers
  // the top-of-page pulse strip ("total / active-now / expired /
  // quota-exhausted"), tuned for fleets with thousands of clients
  // where scrolling-and-scanning is slow.
  const counts = useMemo(() => {
    let active = 0,
      disabled = 0,
      expired = 0,
      online = 0,
      quotaExhausted = 0;
    for (const c of clients) {
      const s = effectiveStatus(c, nowMs);
      if (s === "active") active++;
      else if (s === "disabled") disabled++;
      else expired++;
      if (c.activeTcpConns > 0) online++;
      if (c.dataQuotaBytes > 0 && c.trafficUsedBytes >= c.dataQuotaBytes) quotaExhausted++;
    }
    return { all: clients.length, active, disabled, expired, online, quotaExhausted };
  }, [clients, nowMs]);
  const statusCounts = counts;

  const filtered = useMemo(
    () =>
      clients.filter((c) => {
        const matchSearch = !search || c.name.toLowerCase().includes(search.toLowerCase());
        const matchStatus =
          statusFilter === "all" || effectiveStatus(c, nowMs) === statusFilter;
        return matchSearch && matchStatus;
      }),
    [clients, search, statusFilter, nowMs],
  );

  const totalPages = Math.max(1, Math.ceil(filtered.length / pageSize));
  const safePage = Math.min(currentPage, totalPages);
  const paginated = filtered.slice((safePage - 1) * pageSize, safePage * pageSize);

  // Selection helpers (scoped to the visible page — no fleet-wide select).
  const pageIds = useMemo(() => paginated.map((c) => c.id), [paginated]);
  const selectedOnPage = pageIds.filter((id) => selected.has(id));
  const allSelectedOnPage = pageIds.length > 0 && selectedOnPage.length === pageIds.length;
  const someSelectedOnPage = selectedOnPage.length > 0 && !allSelectedOnPage;
  const toggleOne = (id: string) =>
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  const toggleAllOnPage = () =>
    setSelected((prev) => {
      const next = new Set(prev);
      if (allSelectedOnPage) pageIds.forEach((id) => next.delete(id));
      else pageIds.forEach((id) => next.add(id));
      return next;
    });
  const clearSelection = () => setSelected(new Set());

  const runBulk = async (action: BulkClientAction) => {
    if (!onBulkAction || selected.size === 0) return;
    const ids = Array.from(selected);
    await Promise.resolve(onBulkAction(action, ids));
    clearSelection();
  };

  const columns = buildColumns(nowSec, {
    selected,
    onToggle: toggleOne,
    onToggleAll: toggleAllOnPage,
    allSelected: allSelectedOnPage,
    someSelected: someSelectedOnPage,
  });

  return (
    <>
      <PageHeader
        title="Clients"
        subtitle={`${clients.length} client${clients.length === 1 ? "" : "s"}`}
        trailing={
          onCreate ? (
            <Button
              size="sm"
              onClick={() => {
                setCreateData({ ...emptyFormData });
                setCreateOpen(true);
              }}
            >
              Add Client
            </Button>
          ) : undefined
        }
      />
      <div className="px-4 md:px-8 pb-8 flex flex-col gap-5">
        {!!pendingDiscoveredCount && (
          <DiscoveredClientsBanner count={pendingDiscoveredCount} onClick={onDiscoveredClick} />
        )}

        {/* Pulse row — four key ratios an operator scans before diving into
            the list. Total / active now / expired / quota-exhausted. */}
        <PulseRow
          ticks={[
            {
              label: "Total",
              value: counts.all.toLocaleString(),
              hint: `${counts.disabled.toLocaleString()} disabled`,
            },
            {
              label: "Active now",
              value: counts.online.toLocaleString(),
              hint: "holding connections",
              tone: counts.online > 0 ? "ok" : "default",
            },
            {
              label: "Expired",
              value: counts.expired.toLocaleString(),
              hint: counts.expired > 0 ? "past expiration date" : "none past expiry",
              tone: counts.expired > 0 ? "error" : "default",
            },
            {
              label: "Quota exhausted",
              value: counts.quotaExhausted.toLocaleString(),
              hint:
                counts.quotaExhausted > 0 ? "traffic ≥ quota" : "all within limits",
              tone: counts.quotaExhausted > 0 ? "warn" : "default",
            },
          ] satisfies PulseTick[]}
        />

        <BulkActionBar
          count={selected.size}
          hint="run a bulk action or clear the selection"
          actions={[
            { id: "enable", label: "Enable", variant: "ghost", disabled: !onBulkAction },
            { id: "disable", label: "Disable", variant: "ghost", disabled: !onBulkAction },
            { id: "delete", label: "Delete", variant: "ghost", disabled: !onBulkAction },
          ]}
          onAction={(id) => runBulk(id as BulkClientAction)}
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
            placeholder: "Search by name…",
          }}
          filters={[
            {
              key: "status",
              value: statusFilter,
              onChange: (v) => {
                setStatusFilter(v);
                setCurrentPage(1);
              },
              // Inline chip toggle so the four statuses are one click
              // away — no dropdown step for the most-used filter on a
              // multi-thousand client list.
              variant: "chips",
              options: [
                { value: "all", label: `All · ${statusCounts.all}` },
                { value: "active", label: `Active · ${statusCounts.active}`, tone: "ok" as const },
                {
                  value: "disabled",
                  label: `Disabled · ${statusCounts.disabled}`,
                  tone: "warn" as const,
                },
                {
                  value: "expired",
                  label: `Expired · ${statusCounts.expired}`,
                  tone: "error" as const,
                },
              ],
              placeholder: "Status",
            },
          ]}
          viewMode={
            onViewModeChange ? { current: effectiveMode, onChange: onViewModeChange } : undefined
          }
          pagination={{
            page: safePage,
            totalPages,
            totalItems: filtered.length,
            pageSize,
            onChange: setCurrentPage,
          }}
        >
          <div className="bg-bg-card border border-border rounded-xl shadow-sm overflow-hidden">
            {/* Mobile: compact rows with optional checkboxes. */}
            <div className="md:hidden flex flex-col">
              {paginated.map((c) => (
                <ClientListRow
                  key={c.id}
                  client={c}
                  onClick={() => onClientClick?.(c.id)}
                  selectable
                  selected={selected.has(c.id)}
                  onToggleSelect={toggleOne}
                  nowMs={nowMs}
                />
              ))}
            </div>
            {/* Desktop: DataTable with multi-select column. */}
            <div className="hidden md:block">
              <DataTable
                columns={columns}
                data={paginated}
                keyExtractor={(c) => c.id}
                onRowClick={(c) => onClientClick?.(c.id)}
              />
            </div>
          </div>
        </TableView>
      </div>

      {onCreate && (
        <Sheet
          open={createOpen}
          onOpenChange={(open) => {
            if (!open) setCreateOpen(false);
          }}
        >
          <SheetContent
            side="bottom"
            title="Add client"
            onOpenChange={(open) => {
              if (!open) setCreateOpen(false);
            }}
          >
            <SheetBody>
              <ClientFormSheet
                mode="create"
                data={createData}
                onChange={setCreateData}
                onSubmit={async () => {
                  await onCreate(createData);
                  if (!createError) setCreateOpen(false);
                }}
                onCancel={() => setCreateOpen(false)}
                loading={createLoading}
                error={createError}
                fleetGroups={fleetGroups}
                agents={agents}
              />
            </SheetBody>
          </SheetContent>
        </Sheet>
      )}
    </>
  );
}
