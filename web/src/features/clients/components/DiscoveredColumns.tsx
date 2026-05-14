// R-Q-08: column factory + StatusPill helper extracted from
// DiscoveredClientsPage.tsx. The host page only orchestrates state +
// composition; cell rendering lives here.
//
// R-Q-24: factory + helper co-located by design (same as ClientsPageCells).
/* eslint-disable react-refresh/only-export-components */

import type { TFunction } from "i18next";
import { useTranslation } from "react-i18next";

import {
  Badge,
  Button,
  StatusDot,
  formatBytes,
  formatQuota,
} from "@/ui";
import type { DiscoveredGroup } from "@/features/clients/lib/groupDiscovered";

export function DiscoveredStatusPill({ status }: Readonly<{ status: DiscoveredGroup["status"] }>) {
  const { t } = useTranslation("clients");
  if (status === "adopted") return <Badge variant="ok">{t("discovered.statusPill.adopted")}</Badge>;
  if (status === "ignored")
    return <Badge variant="default">{t("discovered.statusPill.ignored")}</Badge>;
  if (status === "mixed") return <Badge variant="warn">{t("discovered.statusPill.mixed")}</Badge>;
  return <Badge variant="warn">{t("discovered.statusPill.pending")}</Badge>;
}

function rowDotStatus(row: DiscoveredGroup): "ok" | "warn" | "error" {
  if (row.status === "adopted") return "ok";
  if (row.status === "ignored") return "warn";
  if (row.hasConflict) return "error";
  return "warn";
}

export interface DiscoveredColumnsOptions {
  selection?:
    | {
        selected: Set<string>;
        onToggle: (key: string) => void;
        onToggleAll: () => void;
        allSelected: boolean;
        someSelected: boolean;
      }
    | undefined;
  onAdopt?: ((ids: string[]) => void) | undefined;
  onIgnore?: ((ids: string[]) => void) | undefined;
  busy?: boolean | undefined;
  withActions: boolean;
  t: TFunction<"clients">;
}

export function buildDiscoveredColumns(opts: Readonly<DiscoveredColumnsOptions>) {
  const { selection, onAdopt, onIgnore, busy, withActions, t } = opts;
  const cols: Array<{
    key: string;
    header: string;
    render: (row: Readonly<DiscoveredGroup>) => React.ReactNode;
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
          aria-label={t("discovered.table.selectAll")}
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
          aria-label={t("discovered.table.selectOne", { name: row.clientName })}
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
      header: t("discovered.table.client"),
      render: (row) => (
        <div className="flex items-center gap-2 min-w-0">
          <StatusDot status={rowDotStatus(row)} />
          <span className="font-medium text-fg truncate">{row.clientName}</span>
          {(() => {
            if (row.hasNameConflict)
              return <Badge variant="error">{t("discovered.badges.nameConflict")}</Badge>;
            if (row.hasConflict) return <Badge variant="warn">{t("discovered.badges.conflict")}</Badge>;
            return null;
          })()}
        </div>
      ),
      className: "w-[26%]",
    },
    {
      key: "nodes",
      header: t("discovered.table.discoveredOn"),
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
      header: t("discovered.table.usage"),
      render: (row) => (
        <div className="flex flex-col font-mono text-[11px]">
          <span className="text-fg tabular-nums">
            {row.currentConnections} {t("table.connsSuffix")} · {row.activeUniqueIps}{" "}
            {t("table.ipsSuffix")}
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
      header: t("discovered.table.discoveredAt"),
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
      header: t("discovered.table.status"),
      render: (row) => <DiscoveredStatusPill status={row.status} />,
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
            {t("discovered.table.adopt")}
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
            {t("discovered.table.ignore")}
          </Button>
        </div>
      ),
      className: "w-[180px]",
    });
  }

  return cols;
}
