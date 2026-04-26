// Phase-7 redesign: compact columns, status counts in chip labels, bulk
// actions toolbar (enable / disable / delete) with multi-select.
//
// R-Q-08: cell renderers, column factory, and mobile row live in
// `./components/` so this file stays focused on data orchestration.
import { useMemo, useState } from "react";

import { ClientFormSheet } from "@/features/clients/ClientFormSheet";
import { DiscoveredClientsBanner } from "@/features/clients/DiscoveredClientsBanner";
import { ClientListRow } from "@/features/clients/components/ClientListRow";
import { effectiveClientStatus } from "@/features/clients/components/ClientsPageCells";
import { buildClientColumns } from "@/features/clients/components/ClientsTableColumns";
import { useNowSec } from "@/shared/hooks/useNowSec";
import {
  BulkActionBar,
  Button,
  DataTable,
  EmptyState,
  PageHeader,
  PulseRow,
  Sheet,
  SheetBody,
  SheetContent,
  TableView,
  type BulkClientAction,
  type ClientFormData,
  type ClientsPageProps,
  type PulseTick,
  type ViewMode,
} from "@/ui";

const emptyFormData: ClientFormData = {
  name: "",
  userAdTag: "",
  // Default to auto-generation for new clients — operators who want
  // no tag untick the checkbox before saving.
  userAdTagAuto: true,
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
      const s = effectiveClientStatus(c, nowMs);
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
          statusFilter === "all" || effectiveClientStatus(c, nowMs) === statusFilter;
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

  const columns = buildClientColumns(nowSec, {
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

        {clients.length === 0 ? (
          // First-run placeholder. The PageHeader's "Add Client"
          // button already sits above this block, so the empty state
          // only needs to explain what operators should do next.
          <div className="py-10">
            <EmptyState
              icon="👥"
              title="Клиентов пока нет"
              description="Создайте первого клиента, чтобы начать назначать его на ноды Telemt."
            />
          </div>
        ) : (
          <>
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
                    {
                      value: "active",
                      label: `Active · ${statusCounts.active}`,
                      tone: "ok" as const,
                    },
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
                onViewModeChange
                  ? { current: effectiveMode, onChange: onViewModeChange }
                  : undefined
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
          </>
        )}
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
