// P3-FE-01: recomposed locally from UI-kit primitives/components/compositions
// instead of importing the pre-built page from @lost-coder/panvex-ui/pages.
import { useState } from "react";
import {
  Button,
  DataTable,
  NodeCard,
  NodeSummaryCard,
  PageHeader,
  StatusDot,
  TableView,
  cn,
  type ServerListItem,
  type ServersPageProps,
  type ViewMode,
} from "@lost-coder/panvex-ui";

function TrafficCell({ bytes }: { bytes: number }) {
  return (
    <span className="text-sm font-mono text-fg-muted">
      {Math.round(bytes / 1024 / 1024 / 1024)} GB
    </span>
  );
}

function DcMatrixCell({ dcs }: { dcs: ServerListItem["dcs"] }) {
  if (!dcs || dcs.length === 0) return <span className="text-xs text-fg-muted">N/A</span>;
  return (
    <div className="grid grid-cols-6 gap-1 w-fit">
      {dcs.slice(0, 12).map((dc, i) => (
        <div
          key={i}
          className={cn(
            "w-2.5 h-2.5 rounded-full",
            dc.status === "error"
              ? "bg-status-error"
              : dc.status === "warn"
                ? "bg-status-warn"
                : "bg-status-ok opacity-80",
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
  onServerClick?: (id: string) => void;
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

function ServerListView({
  servers,
  onServerClick,
  visibleColumns,
}: {
  servers: ServerListItem[];
  onServerClick?: (id: string) => void;
  visibleColumns: Record<string, boolean>;
}) {
  const allColumns = [
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
      className: "hidden xl:table-cell w-[68px]",
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
  const pageSize = 20;

  const effectiveMode: ViewMode = viewMode ?? (servers.length <= autoThreshold ? "cards" : "list");

  // Filtering
  const filtered = servers.filter((s) => {
    const matchSearch = !search || s.name.toLowerCase().includes(search.toLowerCase());
    const matchStatus = statusFilter === "all" || s.status === statusFilter;
    const matchGroup = groupFilter === "all" || s.fleetGroupId === groupFilter;
    return matchSearch && matchStatus && matchGroup;
  });

  const totalPages = Math.max(1, Math.ceil(filtered.length / pageSize));
  const paginated = filtered.slice((currentPage - 1) * pageSize, currentPage * pageSize);

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
      <div className="px-4 md:px-8 pb-8">
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
              options: [
                { value: "all", label: "All Statuses" },
                { value: "ok", label: "Online" },
                { value: "warn", label: "Warning" },
                { value: "error", label: "Error" },
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
              />
            )}
          </div>
        </TableView>
      </div>
    </>
  );
}
