// Phase-7 redesign: compact columns, status counts in chip labels, bulk
// actions toolbar (enable / disable / delete) with multi-select.
//
// R-Q-08: cell renderers, column factory, and mobile row live in
// `./components/` so this file stays focused on data orchestration.
import { useMemo, useState } from "react";

import { DiscoveredClientsBanner } from "@/features/clients/DiscoveredClientsBanner";
import { ClientsCreateSheet } from "@/features/clients/components/ClientsCreateSheet";
import {
  buildClientsBulkActions,
  buildClientsStatusFilter,
} from "@/features/clients/components/ClientsFilters";
import { effectiveClientStatus } from "@/features/clients/components/ClientsPageCells";
import {
  ClientsPagePulse,
  buildClientCounts,
} from "@/features/clients/components/ClientsPagePulse";
import { ClientsTableBody } from "@/features/clients/components/ClientsTableBody";
import { buildClientColumns } from "@/features/clients/components/ClientsTableColumns";
import { useClientSelection } from "@/features/clients/components/useClientSelection";
import { useNowSec } from "@/shared/hooks/useNowSec";
import {
  BulkActionBar,
  Button,
  EmptyState,
  PageHeader,
  TableView,
  type BulkClientAction,
  type ClientFormData,
  type ClientsPageProps,
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
}: Readonly<ClientsPageProps>) {
  const [search, setSearch] = useState("");
  const [statusFilter, setStatusFilter] = useState("all");
  const [currentPage, setCurrentPage] = useState(1);
  const [createOpen, setCreateOpen] = useState(false);
  const [createData, setCreateData] = useState<ClientFormData>({ ...emptyFormData });
  const pageSize = 20;
  // Auto-refreshing "now" — lifted out of the render path so `effectiveStatus`
  // and `ExpiryCell` stay pure (react-hooks/purity).
  const nowSec = useNowSec();
  const nowMs = nowSec * 1000;

  const effectiveMode: ViewMode = viewMode ?? (clients.length <= autoThreshold ? "cards" : "list");

  // Counts power both the top-of-page pulse strip and the chip filter
  // labels. Memo on the unfiltered list so the chips keep the full
  // distribution regardless of the active filter.
  const counts = useMemo(() => buildClientCounts(clients, nowMs), [clients, nowMs]);
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
  const sel = useClientSelection(pageIds);

  const runBulk = async (action: Readonly<BulkClientAction>) => {
    if (!onBulkAction || sel.selected.size === 0) return;
    const ids = Array.from(sel.selected);
    await Promise.resolve(onBulkAction(action, ids));
    sel.clear();
  };

  const columns = buildClientColumns(nowSec, {
    selected: sel.selected,
    onToggle: sel.toggleOne,
    onToggleAll: sel.toggleAllOnPage,
    allSelected: sel.allSelected,
    someSelected: sel.someSelected,
  });

  return (
    <>
      <PageHeader
        title="Clients"
        subtitle={`${clients.length} client${clients.length === 1 ? "" : "s"}`}
        trailing={
          <div className="flex items-center gap-2">
            {onDiscoveredClick && (
              <Button
                size="sm"
                variant="outline"
                onClick={onDiscoveredClick}
                title="Review clients running on agents that the panel hasn't adopted yet"
              >
                Discovered
                {pendingDiscoveredCount ? (
                  <span className="ml-1.5 inline-flex items-center justify-center min-w-[18px] h-[18px] px-1 rounded-full bg-accent text-bg text-[10px] font-mono">
                    {pendingDiscoveredCount}
                  </span>
                ) : null}
              </Button>
            )}
            {onCreate && (
              <Button
                size="sm"
                onClick={() => {
                  setCreateData({ ...emptyFormData });
                  setCreateOpen(true);
                }}
              >
                Add Client
              </Button>
            )}
          </div>
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
            {/* Pulse row — four key ratios an operator scans before diving
                into the list. Total / active now / expired / quota-exhausted. */}
            <ClientsPagePulse counts={counts} />

            <BulkActionBar
              count={sel.selected.size}
              hint="run a bulk action or clear the selection"
              actions={buildClientsBulkActions(!!onBulkAction)}
              onAction={(id) => runBulk(id as BulkClientAction)}
              onClear={sel.clear}
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
                buildClientsStatusFilter({
                  value: statusFilter,
                  onChange: (v) => {
                    setStatusFilter(v);
                    setCurrentPage(1);
                  },
                  counts: statusCounts,
                }),
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
              <ClientsTableBody
                rows={paginated}
                columns={columns}
                selection={{
                  selected: sel.selected,
                  onToggle: sel.toggleOne,
                  onToggleAll: sel.toggleAllOnPage,
                  allSelected: sel.allSelected,
                  someSelected: sel.someSelected,
                }}
                onClientClick={onClientClick}
                nowMs={nowMs}
              />
            </TableView>
          </>
        )}
      </div>

      {onCreate && (
        <ClientsCreateSheet
          open={createOpen}
          data={createData}
          onChange={setCreateData}
          onSubmit={async () => {
            await onCreate(createData);
            if (!createError) setCreateOpen(false);
          }}
          onClose={() => setCreateOpen(false)}
          loading={createLoading}
          error={createError}
          fleetGroups={fleetGroups}
          agents={agents}
        />
      )}
    </>
  );
}
