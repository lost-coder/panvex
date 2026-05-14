// ServersPage — composed locally from UI-kit primitives, components, and
// compositions. Page composition is feature-side; the kit at `@/ui` only
// ships the building blocks.
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";

import {
  BulkActionBar,
  Button,
  PageHeader,
  TableView,
  type BulkServerAction,
  type ServersPageProps,
  type ViewMode,
} from "@/ui";
import { ServerCardView } from "./ui/ServerCardView";
import { ServerListView } from "./ui/ServerListView";

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
}: Readonly<ServersPageProps>) {
  const { t } = useTranslation("servers");
  const [search, setSearch] = useState("");
  const [statusFilter, setStatusFilter] = useState("all");
  const [groupFilter, setGroupFilter] = useState("all");
  const [currentPage, setCurrentPage] = useState(1);
  const [columnVisibility, setColumnVisibility] = useState<Record<string, boolean>>({
    transport: true,
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

  const runBulk = async (action: Readonly<BulkServerAction>) => {
    if (!onBulkAction || selected.size === 0) return;
    const ids = Array.from(selected);
    await Promise.resolve(onBulkAction(action, ids));
    clearSelection();
  };

  return (
    <>
      <PageHeader
        title={t("page.title")}
        subtitle={t("page.subtitle", { count: servers.length })}
        trailing={
          onManageTokens || onAddServer ? (
            <div className="flex items-center gap-2">
              {onManageTokens && (
                <Button variant="ghost" size="sm" onClick={onManageTokens}>
                  {t("page.manageTokens")}
                </Button>
              )}
              {onAddServer && (
                <Button size="sm" onClick={onAddServer}>
                  {t("page.addServer")}
                </Button>
              )}
            </div>
          ) : undefined
        }
      />
      <div className="px-4 md:px-8 pb-8 flex flex-col gap-5">
        <BulkActionBar
          count={selected.size}
          hint={t("list.bulk.hint")}
          actions={[
            {
              id: "reload",
              label: t("list.bulk.reload"),
              variant: "ghost",
              disabled: !onBulkAction,
            },
            {
              id: "selfUpdate",
              label: t("list.bulk.selfUpdate"),
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
            placeholder: t("list.filter.searchPlaceholder"),
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
                { value: "all", label: t("list.filter.all", { count: statusCounts.all }) },
                { value: "ok", label: t("list.filter.ok", { count: statusCounts.ok }), tone: "ok" as const },
                { value: "warn", label: t("list.filter.warn", { count: statusCounts.warn }), tone: "warn" as const },
                { value: "error", label: t("list.filter.error", { count: statusCounts.error }), tone: "error" as const },
              ],
              placeholder: t("list.filter.statusPlaceholder"),
            },
            {
              key: "group",
              value: groupFilter,
              onChange: (v) => {
                setGroupFilter(v);
                setCurrentPage(1);
              },
              options: [
                { value: "all", label: t("list.filter.allGroups") },
                ...(fleetGroups ?? []).map((g) => ({
                  value: g.id,
                  label: g.label ?? g.name ?? g.id,
                })),
              ],
              placeholder: t("list.filter.groupPlaceholder"),
            },
          ]}
          viewMode={
            onViewModeChange ? { current: effectiveMode, onChange: onViewModeChange } : undefined
          }
          columns={{
            available: [
              { key: "transport", label: t("list.columns.transport") },
              { key: "users", label: t("list.columns.users") },
              { key: "traffic", label: t("list.columns.traffic") },
              { key: "uptime", label: t("list.columns.uptime") },
              { key: "load", label: t("list.columns.load") },
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
