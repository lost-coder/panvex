import { useMemo } from "react";
import { NodeCard } from "@/features/servers/ui/NodeCard";
import { TransportBadge } from "@/features/servers/ui/TransportBadge";
import { classifyMode } from "@/features/servers/server-detail/classifyMode";
import {
  DataTable,
  StatusDot,
  type ServerListItem,
} from "@/ui";

export function TrafficCell({ bytes }: Readonly<{ bytes: number }>) {
  return (
    <span className="text-sm font-mono text-fg-muted">
      {Math.round(bytes / 1024 / 1024 / 1024)} GB
    </span>
  );
}

export interface ServerSelectionConfig {
  selected: Set<string>;
  onToggle: (id: string) => void;
  onToggleAll: () => void;
  allSelected: boolean;
  someSelected: boolean;
}

export function ServerListView({
  servers,
  onServerClick,
  visibleColumns,
  selection,
}: Readonly<{
  servers: ServerListItem[];
  onServerClick?: ((id: string) => void) | undefined;
  visibleColumns: Record<string, boolean>;
  selection?: ServerSelectionConfig | undefined;
}>) {
  // L-20: column array embeds inline render lambdas — without useMemo
  // every parent rerender produces a fresh array reference, defeating
  // any memoisation downstream of `columns`.
  const allColumns = useMemo(() => [
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
            render: (s: Readonly<ServerListItem>) => (
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
      render: (s: Readonly<ServerListItem>) => (
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
      key: "transport",
      header: "Transport",
      render: (s: Readonly<ServerListItem>) => (
        <TransportBadge
          mode={classifyMode({
            useMiddleProxy: s.useMiddleProxy,
            meRuntimeReady: s.meRuntimeReady,
            me2dcFallbackEnabled: s.me2dcFallbackEnabled,
          })}
          healthy={s.healthyUpstreams}
          total={s.totalUpstreams}
          severity={s.severity}
        />
      ),
      className: "hidden xl:table-cell w-[140px]",
    },
    {
      key: "users",
      header: "Users",
      render: (s: Readonly<ServerListItem>) => (
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
      render: (s: Readonly<ServerListItem>) => (
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
      render: (s: Readonly<ServerListItem>) => {
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
      render: (s: Readonly<ServerListItem>) => (
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
  ], [selection]);

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
            mode={classifyMode({
              useMiddleProxy: s.useMiddleProxy,
              meRuntimeReady: s.meRuntimeReady,
              me2dcFallbackEnabled: s.me2dcFallbackEnabled,
            })}
            healthyUpstreams={s.healthyUpstreams}
            totalUpstreams={s.totalUpstreams}
            severity={s.severity}
            cpu={s.cpuPct}
            mem={s.memPct}
            clients={s.connections}
            region="Global"
            idle={s.connections === 0}
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
