import type { ReactNode } from "react";
import { DataTable } from "@/components/data-table";
import type {
  ClientTableRow,
  ClientTableSortDir,
  ClientTableSortKey,
} from "./clients-view-model";

import "./client-table.css";

interface ClientTableProps {
  rows: ClientTableRow[];
  sortKey: ClientTableSortKey;
  sortDir: ClientTableSortDir;
  onSort: (key: ClientTableSortKey) => void;
  onRowClick?: (row: ClientTableRow) => void;
  footer?: ReactNode;
}

export function ClientTable({
  rows,
  sortKey,
  sortDir,
  onSort,
  onRowClick,
  footer,
}: ClientTableProps) {
  const columns = [
    {
      key: "client",
      label: "Client",
      sortable: true,
      headerClassName: "col-client-head",
      cellClassName: "col-client",
      render: (row: ClientTableRow) => (
        <div className="cell-client">
          <div className="cell-client-name">{row.clientName}</div>
          <div className="cell-client-meta">{row.deployStatusText}</div>
        </div>
      ),
    },
    {
      key: "status",
      label: "Status",
      sortable: true,
      headerClassName: "col-status-head",
      cellClassName: "col-status",
      render: (row: ClientTableRow) => (
        <span className={`client-table__status-badge ${row.statusTone}`}>
          <span className="client-table__status-badge-dot" />
          {row.statusText}
        </span>
      ),
    },
    {
      key: "connections",
      label: "Connections",
      sortable: true,
      headerClassName: "col-connections-head",
      cellClassName: "col-connections",
      mobileLabel: "Connections",
      render: (row: ClientTableRow) => <span className="cell-mono">{row.connectionsText}</span>,
    },
    {
      key: "servers",
      label: "Servers",
      sortable: true,
      headerClassName: "col-servers-head",
      cellClassName: "col-servers",
      mobileLabel: "Servers",
      render: (row: ClientTableRow) => <span className="cell-mono">{row.serversText}</span>,
    },
    {
      key: "traffic",
      label: "Traffic",
      sortable: true,
      headerClassName: "col-traffic-head",
      cellClassName: "col-traffic",
      mobileLabel: "Traffic",
      render: (row: ClientTableRow) => <span className="cell-mono">{row.trafficText}</span>,
    },
    {
      key: "quota",
      label: "Quota",
      sortable: true,
      headerClassName: "col-quota-head",
      cellClassName: "col-quota",
      mobileLabel: "Quota",
      render: (row: ClientTableRow) => (
        <span className={row.quotaText === "—" ? "cell-mono cell-dim" : "cell-mono"}>
          {row.quotaText}
        </span>
      ),
    },
    {
      key: "expires",
      label: "Expires",
      sortable: true,
      headerClassName: "col-expires-head",
      cellClassName: "col-expires",
      mobileLabel: "Expires",
      render: (row: ClientTableRow) => (
        <div className="client-table__expires">
          <span className="client-table__expires-primary">{row.expiresPrimaryText}</span>
          <span className="client-table__expires-secondary">{row.expiresSecondaryText}</span>
        </div>
      ),
    },
  ];

  return (
    <DataTable
      columns={columns}
      emptyMessage="No clients match the current filters."
      footer={footer}
      headerRowClassName="client-table-header-row"
      onRowClick={onRowClick}
      onSort={(key) => onSort(key as ClientTableSortKey)}
      rowClassName="client-table-row"
      rows={rows}
      sortDir={sortDir}
      sortKey={sortKey}
      tableClassName="client-table-grid"
      wrapperClassName="client-table-surface"
    />
  );
}
