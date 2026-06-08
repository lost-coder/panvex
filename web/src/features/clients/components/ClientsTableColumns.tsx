// R-Q-08: columns factory extracted from ClientsPage.tsx. Returns the
// column descriptors consumed by DataTable; the host page passes the
// "now" snapshot + selection config so the factory stays pure.

import type { TFunction } from "i18next";

import { MonoValue, StatusDot, type ClientListItem } from "@/ui";

import {
  ClientExpiryCell,
  ClientStateBadge,
  ClientTrafficCell,
  deriveClientState,
} from "./ClientsPageCells";

export interface ClientSelectionConfig {
  selected: Set<string>;
  onToggle: (id: string) => void;
  onToggleAll: () => void;
  allSelected: boolean;
  someSelected: boolean;
}

export function buildClientColumns(
  nowSec: number,
  t: TFunction<"clients">,
  selection?: ClientSelectionConfig,
) {
  return [
    ...(selection
      ? [
          {
            key: "select",
            header: (
              <input
                type="checkbox"
                aria-label={t("table.selectAll")}
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
                aria-label={t("table.selectOne", { name: c.name })}
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
      header: t("table.client"),
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
      header: t("table.status"),
      render: (c: ClientListItem) => (
        <ClientStateBadge state={deriveClientState(c, nowSec * 1000)} />
      ),
      className: "w-[120px]",
    },
    {
      key: "usage",
      header: t("table.usage"),
      render: (c: ClientListItem) => (
        <div className="flex flex-col font-mono text-micro">
          <span className="text-fg tabular-nums">
            {c.activeTcpConns} {t("table.connsSuffix")}
          </span>
          <span className="text-fg-muted tabular-nums">
            {c.uniqueIpsUsed} {t("table.ipsSuffix")}
          </span>
        </div>
      ),
      className: "hidden md:table-cell w-[110px]",
    },
    {
      key: "traffic",
      header: t("table.traffic"),
      render: (c: ClientListItem) => (
        <ClientTrafficCell used={c.trafficUsedBytes} quota={c.dataQuotaBytes} nodes={c.assignedNodesCount} />
      ),
      className: "hidden lg:table-cell w-[180px]",
    },
    {
      key: "expires",
      header: t("table.expires"),
      render: (c: ClientListItem) => (
        <ClientExpiryCell rfc={c.expirationRfc3339} nowSec={nowSec} t={t} />
      ),
      className: "hidden md:table-cell w-[120px]",
    },
    {
      key: "nodes",
      header: t("table.nodes"),
      render: (c: ClientListItem) => (
        <MonoValue className="text-fg-muted">{c.assignedNodesCount}</MonoValue>
      ),
      className: "hidden xl:table-cell w-[80px] text-center",
    },
  ];
}
