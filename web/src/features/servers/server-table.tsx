import type { ReactNode } from "react";
import { Server as ServerIcon } from "lucide-react";
import { DataTable } from "@/components/data-table";
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
}

export function ServerTable({
  rows,
  sortKey,
  sortDir,
  onSort,
  onRowClick,
  footer,
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
      label: "Status",
      sortable: true,
      headerClassName: "col-status-head",
      cellClassName: "col-status",
      render: (row: ServerTableRow) => (
        <span className={`server-table__status-badge ${row.statusTone}`}>
          <span className="server-table__status-badge-dot" />
          {row.statusText}
        </span>
      ),
    },
    {
      key: "clients",
      label: "Clients",
      sortable: true,
      headerClassName: "col-clients-head",
      cellClassName: "col-clients",
      mobileLabel: "Clients",
      render: (row: ServerTableRow) => (
        <span className={row.clientsText === "—" ? "cell-mono cell-dim" : "cell-mono"}>
          {row.clientsText}
        </span>
      ),
    },
    {
      key: "cpu",
      label: "CPU",
      sortable: true,
      headerClassName: "col-cpu-head",
      cellClassName: "col-cpu",
      mobileLabel: "CPU",
      render: (row: ServerTableRow) => <span className="cell-mono cell-dim">{row.cpuText}</span>,
    },
    {
      key: "memory",
      label: "Memory",
      sortable: true,
      headerClassName: "col-memory-head",
      cellClassName: "col-memory",
      mobileLabel: "Memory",
      render: (row: ServerTableRow) => <span className="cell-mono cell-dim">{row.memoryText}</span>,
    },
    {
      key: "dc",
      label: "DC",
      sortable: true,
      headerClassName: "col-dc-head",
      cellClassName: "col-dc",
      mobileLabel: "DC",
      render: (row: ServerTableRow) => (
        <div className="server-table__dc-summary">
          <div className="server-table__dc-bar" aria-hidden="true">
            {row.dcSegments.map((segment, index) => (
              <span
                key={`${row.id}-dc-${index}`}
                className={`server-table__dc-segment ${segment}`}
              />
            ))}
          </div>
          <span className="server-table__dc-text">{row.dcSummaryText}</span>
        </div>
      ),
    },
    {
      key: "traffic",
      label: "Traffic",
      sortable: true,
      headerClassName: "col-traffic-head",
      cellClassName: "col-traffic",
      mobileLabel: "Traffic",
      render: (row: ServerTableRow) => <span className="cell-mono cell-dim">{row.trafficText}</span>,
    },
    {
      key: "uptime",
      label: "Uptime",
      sortable: true,
      headerClassName: "col-uptime-head",
      cellClassName: "col-uptime",
      mobileLabel: "Uptime",
      render: (row: ServerTableRow) => <span className="cell-mono cell-dim">{row.uptimeText}</span>,
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
