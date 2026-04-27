// R-Q-08: column factory + StatusPill helper extracted from
// DiscoveredClientsPage.tsx. The host page only orchestrates state +
// composition; cell rendering lives here.
//
// R-Q-24: factory + helper co-located by design (same as ClientsPageCells).
/* eslint-disable react-refresh/only-export-components */

import {
  Badge,
  Button,
  StatusDot,
  formatBytes,
  formatQuota,
} from "@/ui";
import type { DiscoveredGroup } from "@/features/clients/lib/groupDiscovered";

export function DiscoveredStatusPill({ status }: Readonly<{ status: DiscoveredGroup["status"] }>) {
  if (status === "adopted") return <Badge variant="ok">Adopted</Badge>;
  if (status === "ignored") return <Badge variant="default">Ignored</Badge>;
  if (status === "mixed") return <Badge variant="warn">Mixed</Badge>;
  return <Badge variant="warn">Pending</Badge>;
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
}

export function buildDiscoveredColumns(opts: Readonly<DiscoveredColumnsOptions>) {
  const { selection, onAdopt, onIgnore, busy, withActions } = opts;
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
          aria-label="Select all on this page"
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
          aria-label={`Select ${row.clientName}`}
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
      header: "Client",
      render: (row) => (
        <div className="flex items-center gap-2 min-w-0">
          <StatusDot
            status={
              row.status === "adopted"
                ? "ok"
                : row.status === "ignored"
                  ? "warn"
                  : row.hasConflict
                    ? "error"
                    : "warn"
            }
          />
          <span className="font-medium text-fg truncate">{row.clientName}</span>
          {row.hasNameConflict ? (
            <Badge variant="error">name conflict</Badge>
          ) : row.hasConflict ? (
            <Badge variant="warn">conflict</Badge>
          ) : null}
        </div>
      ),
      className: "w-[26%]",
    },
    {
      key: "nodes",
      header: "Discovered on",
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
      header: "Usage",
      render: (row) => (
        <div className="flex flex-col font-mono text-[11px]">
          <span className="text-fg tabular-nums">
            {row.currentConnections} conns · {row.activeUniqueIps} IPs
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
      header: "Discovered at",
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
      header: "Status",
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
            Adopt
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
            Ignore
          </Button>
        </div>
      ),
      className: "w-[180px]",
    });
  }

  return cols;
}
