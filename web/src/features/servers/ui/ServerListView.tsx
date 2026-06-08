import { useMemo } from "react";
import { useTranslation } from "react-i18next";

import { NodeCard } from "@/features/servers/ui/NodeCard";
import { TransportBadge } from "@/features/servers/ui/TransportBadge";
import { classifyMode } from "@/features/servers/server-detail/classifyMode";
import {
  DataTable,
  NodeStateBadge,
  nodeStatePresentation,
  localizeReason,
  type ServerListItem,
} from "@/ui";

// formatTrafficBytes picks the largest unit that yields a non-zero
// integer-ish number. Returning "0" for anything below 1 MB lets a
// freshly-deployed fleet still show a meaningful column without the
// "always 0 GB" rounding artefact the legacy implementation produced.
function formatTrafficBytes(bytes: number): string {
  if (bytes <= 0) return "0";
  const TB = 1024 ** 4;
  const GB = 1024 ** 3;
  const MB = 1024 ** 2;
  const KB = 1024;
  if (bytes >= TB) return `${(bytes / TB).toFixed(2)} TB`;
  if (bytes >= GB) return `${(bytes / GB).toFixed(2)} GB`;
  if (bytes >= MB) return `${(bytes / MB).toFixed(1)} MB`;
  if (bytes >= KB) return `${Math.round(bytes / KB)} KB`;
  return `${bytes} B`;
}

export function TrafficCell({ bytes }: Readonly<{ bytes: number }>) {
  return (
    <span className="text-sm font-mono text-fg-muted">
      {formatTrafficBytes(bytes)}
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
  const { t } = useTranslation("servers");
  const { t: tc } = useTranslation("common");
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
                aria-label={t("list.card.selectAll")}
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
                aria-label={t("list.card.selectOne", { name: s.name })}
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
      header: t("list.columns.server"),
      render: (s: Readonly<ServerListItem>) => {
        const reasonText = s.reason ? localizeReason(s.reason, tc) : "";
        return (
          <div className="flex flex-col gap-0.5 min-w-0">
            <div className="flex items-center gap-2 min-w-0">
              <NodeStateBadge state={s.state} label={tc(nodeStatePresentation(s.state).labelKey)} />
              <span className="text-sm font-medium text-fg truncate">{s.name}</span>
            </div>
            {reasonText ? (
              <span className="text-xs text-fg-muted truncate">{reasonText}</span>
            ) : (
              s.ip && <span className="pl-[14px] text-nano text-fg-muted font-mono">{s.ip}</span>
            )}
          </div>
        );
      },
      sortable: true,
      sortValue: (s: Readonly<ServerListItem>) => s.name,
      className: "w-[30%]",
    },
    {
      key: "transport",
      header: t("list.columns.transport"),
      render: (s: Readonly<ServerListItem>) => (
        <TransportBadge
          mode={classifyMode({
            useMiddleProxy: s.useMiddleProxy,
            meRuntimeReady: s.meRuntimeReady,
            me2dcFallbackEnabled: s.me2dcFallbackEnabled,
          })}
          healthy={s.totalDcs > 0 ? s.healthyDcs : s.healthyUpstreams}
          total={s.totalDcs > 0 ? s.totalDcs : s.totalUpstreams}
          severity={s.severity}
        />
      ),
      className: "hidden xl:table-cell w-[140px]",
    },
    {
      key: "users",
      header: t("list.columns.users"),
      render: (s: Readonly<ServerListItem>) => {
        const online = s.usersOnline ?? 0;
        const configured = s.usersTotal ?? 0;
        return (
          <div className="flex items-baseline gap-1 font-mono whitespace-nowrap justify-center">
            <span className="text-sm text-fg">{online.toLocaleString()}</span>
            {configured > 0 && (
              <span className="text-xs text-fg-muted">/{configured.toLocaleString()}</span>
            )}
          </div>
        );
      },
      sortable: true,
      sortValue: (s: Readonly<ServerListItem>) => s.usersOnline ?? 0,
      className: "hidden sm:table-cell text-center w-[110px]",
    },
    {
      key: "traffic",
      header: t("list.columns.traffic"),
      render: (s: Readonly<ServerListItem>) => (
        <div className="flex justify-center">
          <TrafficCell bytes={s.trafficBytes} />
        </div>
      ),
      sortable: true,
      sortValue: (s: Readonly<ServerListItem>) => s.trafficBytes,
      className: "hidden md:table-cell text-center w-[80px]",
    },
    {
      key: "uptime",
      header: t("list.columns.uptime"),
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
      sortValue: (s: Readonly<ServerListItem>) => s.uptimeSeconds,
      className: "hidden lg:table-cell text-center w-[70px]",
    },
    {
      key: "load",
      header: t("list.columns.load"),
      render: (s: Readonly<ServerListItem>) => (
        <div className="flex flex-col gap-1">
          <div className="flex items-center gap-1.5 text-nano font-mono leading-none">
            <span className="w-7 text-fg-muted shrink-0">{t("list.columns.cpu")}</span>
            <div className="h-1.5 flex-1 bg-border rounded-full overflow-hidden">
              <div className="h-full bg-fg rounded-full" style={{ width: `${s.cpuPct}%` }} />
            </div>
            <span className="text-fg-muted w-7 text-right shrink-0">{s.cpuPct}%</span>
          </div>
          <div className="flex items-center gap-1.5 text-nano font-mono leading-none">
            <span className="w-7 text-fg-muted shrink-0">{t("list.columns.mem")}</span>
            <div className="h-1.5 flex-1 bg-border rounded-full overflow-hidden">
              <div className="h-full bg-fg-muted rounded-full" style={{ width: `${s.memPct}%` }} />
            </div>
            <span className="text-fg-muted w-7 text-right shrink-0">{s.memPct}%</span>
          </div>
        </div>
      ),
      className: "hidden lg:table-cell w-[140px]",
    },
  ], [selection, t, tc]);

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
            state={s.state}
            reason={s.reason ? localizeReason(s.reason, tc) : ""}
            mode={classifyMode({
              useMiddleProxy: s.useMiddleProxy,
              meRuntimeReady: s.meRuntimeReady,
              me2dcFallbackEnabled: s.me2dcFallbackEnabled,
            })}
            healthyUpstreams={s.totalDcs > 0 ? s.healthyDcs : s.healthyUpstreams}
            totalUpstreams={s.totalDcs > 0 ? s.totalDcs : s.totalUpstreams}
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
