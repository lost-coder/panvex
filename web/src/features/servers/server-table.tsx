import type { ReactNode } from "react";
import { Server as ServerIcon } from "lucide-react";
import { DataTable } from "@/components/data-table";
import { getTelemetryFieldHelp, type TelemetryHelpMode } from "../telemetry/help-metadata";
import type {
  ServerTableRow,
  ServerTableSortDir,
  ServerTableSortKey,
} from "./servers-view-model";

import "./server-table.css";

interface ServerTableProps {
  rows: ServerTableRow[];
  sortKey: ServerTableSortKey;
  sortDir: ServerTableSortDir;
  onSort: (key: ServerTableSortKey) => void;
  onRowClick?: (row: ServerTableRow) => void;
  footer?: ReactNode;
  helpMode?: TelemetryHelpMode;
}

export function ServerTable({
  rows,
  sortKey,
  sortDir,
  onSort,
  onRowClick,
  footer,
  helpMode = "basic",
}: ServerTableProps) {
  const columns = [
    {
      key: "server",
      label: "Server",
      sortable: true,
      headerClassName: "col-server-head",
      cellClassName: "col-server",
      render: (row: ServerTableRow) => (
        <div className="cell-server">
          <div className={`cell-server-icon ${row.statusTone}`}>
            <ServerIcon className="h-4 w-4" />
          </div>
          <div className="min-w-0">
            <div className="cell-server-name">{row.serverName}</div>
            <div className="cell-server-group">{row.groupText}</div>
          </div>
        </div>
      ),
    },
    {
      key: "status",
      label: "Health",
      sortable: true,
      headerClassName: "col-status-head",
      cellClassName: "col-status",
      render: (row: ServerTableRow) => (
        <div className="space-y-1">
          <span className={`server-table__status-badge ${row.statusTone}`}>
            <span className="server-table__status-badge-dot" />
            {row.statusText}
          </span>
          <div className="text-xs text-text-3">{row.reasonText}</div>
          {helpMode === "full" && getTelemetryFieldHelp("Health") ? (
            <div className="text-[10px] leading-4 text-text-3">{getTelemetryFieldHelp("Health")}</div>
          ) : null}
        </div>
      ),
    },
    {
      key: "freshness",
      label: "Freshness",
      sortable: true,
      headerClassName: "col-clients-head",
      cellClassName: "col-clients",
      mobileLabel: "Freshness",
      render: (row: ServerTableRow) => (
        <div className="space-y-1">
          <span className="cell-mono">{row.freshnessText}</span>
          {helpMode === "full" && getTelemetryFieldHelp("Freshness") ? (
            <div className="text-[10px] leading-4 text-text-3">{getTelemetryFieldHelp("Freshness")}</div>
          ) : null}
        </div>
      ),
    },
    {
      key: "runtime",
      label: "Runtime",
      sortable: true,
      headerClassName: "col-cpu-head",
      cellClassName: "col-cpu",
      mobileLabel: "Runtime",
      render: (row: ServerTableRow) => <span className="text-sm text-text-2">{row.runtimeText}</span>,
    },
    {
      key: "dc",
      label: "DC Health",
      sortable: true,
      headerClassName: "col-memory-head",
      cellClassName: "col-memory",
      mobileLabel: "DC",
      render: (row: ServerTableRow) => <span className="text-sm text-text-2">{row.dcSummaryText}</span>,
    },
    {
      key: "upstreams",
      label: "Upstreams",
      sortable: true,
      headerClassName: "col-dc-head",
      cellClassName: "col-dc",
      mobileLabel: "Upstreams",
      render: (row: ServerTableRow) => (
        <div className="space-y-1">
          <span className="text-sm text-text-2">{row.upstreamSummaryText}</span>
          {helpMode === "full" && getTelemetryFieldHelp("Upstreams") ? (
            <div className="text-[10px] leading-4 text-text-3">{getTelemetryFieldHelp("Upstreams")}</div>
          ) : null}
        </div>
      ),
    },
    {
      key: "events",
      label: "Events",
      sortable: true,
      headerClassName: "col-traffic-head",
      cellClassName: "col-traffic",
      mobileLabel: "Events",
      render: (row: ServerTableRow) => <span className="text-sm text-text-2">{row.eventText}</span>,
    },
  ];

  return (
    <DataTable
      columns={columns}
      emptyMessage="No servers match the current filters."
      footer={footer}
      headerRowClassName="server-table-header-row"
      onRowClick={onRowClick}
      onSort={(key) => onSort(key as ServerTableSortKey)}
      rowClassName="server-table-row"
      rows={rows}
      sortDir={sortDir}
      sortKey={sortKey}
      tableClassName="server-table-grid"
      wrapperClassName="server-table-surface"
    />
  );
}
