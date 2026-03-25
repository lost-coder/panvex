import type { ClientListItem } from "../../lib/api";

export type ClientTableFilter = "all" | "active" | "idle" | "disabled";
export type ClientTableSortKey = "client" | "status" | "connections" | "servers" | "traffic" | "quota" | "expires";
export type ClientTableSortDir = "asc" | "desc";
export type ClientStatusTone = "active" | "idle" | "disabled";

export interface ClientTableRow {
  id: string;
  clientName: string;
  deployStatusText: string;
  statusText: string;
  statusTone: ClientStatusTone;
  statusRank: number;
  connectionsValue: number;
  connectionsText: string;
  serversValue: number;
  serversText: string;
  trafficValue: number;
  trafficText: string;
  quotaValue: number;
  quotaText: string;
  expiresTimestamp: number;
  expiresPrimaryText: string;
  expiresSecondaryText: string;
}

const numberFormatter = new Intl.NumberFormat("en-US");
const dateFormatter = new Intl.DateTimeFormat("en-US", {
  day: "2-digit",
  month: "short",
  timeZone: "UTC",
  year: "numeric",
});

export function buildClientTableRows(clients: ClientListItem[], now = Date.now()): ClientTableRow[] {
  return clients.map((client) => {
    const statusTone = resolveClientStatusTone(client);
    const expiresTimestamp = Date.parse(client.expiration_rfc3339);

    return {
      id: client.id,
      clientName: client.name,
      deployStatusText: formatDeployStatus(client.last_deploy_status),
      statusText: statusTone === "active" ? "Active" : statusTone === "idle" ? "Idle" : "Disabled",
      statusTone,
      statusRank: statusTone === "active" ? 1 : statusTone === "idle" ? 2 : 3,
      connectionsValue: client.active_tcp_conns,
      connectionsText: numberFormatter.format(client.active_tcp_conns),
      serversValue: client.assigned_nodes_count,
      serversText: numberFormatter.format(client.assigned_nodes_count ?? 0),
      trafficValue: client.traffic_used_bytes,
      trafficText: formatBytes(client.traffic_used_bytes),
      quotaValue: client.data_quota_bytes,
      quotaText: client.data_quota_bytes > 0
        ? `${formatBytes(client.traffic_used_bytes)} / ${formatBytes(client.data_quota_bytes)}`
        : "—",
      expiresTimestamp,
      expiresPrimaryText: formatRelativeExpiry(expiresTimestamp, now),
      expiresSecondaryText: Number.isFinite(expiresTimestamp) ? dateFormatter.format(expiresTimestamp) : "—",
    };
  });
}

export function buildClientFilterCounts(rows: ClientTableRow[]) {
  return {
    all: rows.length,
    active: rows.filter((row) => row.statusTone === "active").length,
    idle: rows.filter((row) => row.statusTone === "idle").length,
    disabled: rows.filter((row) => row.statusTone === "disabled").length,
  };
}

export function filterClientTableRows(
  rows: ClientTableRow[],
  options: { filter: ClientTableFilter; search: string }
) {
  const normalizedSearch = options.search.trim().toLowerCase();

  return rows.filter((row) => {
    const matchesFilter =
      options.filter === "all" ||
      row.statusTone === options.filter;

    if (!matchesFilter) {
      return false;
    }

    if (!normalizedSearch) {
      return true;
    }

    return [
      row.clientName,
      row.deployStatusText,
      row.statusText,
    ].some((value) => value.toLowerCase().includes(normalizedSearch));
  });
}

export function sortClientTableRows(
  rows: ClientTableRow[],
  sortKey: ClientTableSortKey,
  sortDir: ClientTableSortDir
) {
  const direction = sortDir === "asc" ? 1 : -1;

  return [...rows].sort((leftRow, rightRow) => {
    const comparison = compareByKey(leftRow, rightRow, sortKey);

    if (comparison !== 0) {
      return comparison * direction;
    }

    return leftRow.clientName.localeCompare(rightRow.clientName, "en", { sensitivity: "base" });
  });
}

export function paginateClientTableRows(rows: ClientTableRow[], page: number, pageSize: number) {
  const totalPages = Math.max(1, Math.ceil(rows.length / pageSize));
  const safePage = Math.min(Math.max(page, 1), totalPages);
  const startIndex = (safePage - 1) * pageSize;

  return {
    rows: rows.slice(startIndex, startIndex + pageSize),
    totalPages,
  };
}

function resolveClientStatusTone(client: ClientListItem): ClientStatusTone {
  if (!client.enabled) {
    return "disabled";
  }

  if (client.active_tcp_conns > 0) {
    return "active";
  }

  return "idle";
}

function compareByKey(leftRow: ClientTableRow, rightRow: ClientTableRow, sortKey: ClientTableSortKey) {
  switch (sortKey) {
    case "status":
      return leftRow.statusRank - rightRow.statusRank;
    case "connections":
      return leftRow.connectionsValue - rightRow.connectionsValue;
    case "servers":
      return leftRow.serversValue - rightRow.serversValue;
    case "traffic":
      return leftRow.trafficValue - rightRow.trafficValue;
    case "quota":
      return leftRow.quotaValue - rightRow.quotaValue;
    case "expires":
      return leftRow.expiresTimestamp - rightRow.expiresTimestamp;
    case "client":
    default:
      return leftRow.clientName.localeCompare(rightRow.clientName, "en", { sensitivity: "base" });
  }
}

function formatBytes(bytes: number): string {
  const units = ["B", "KB", "MB", "GB", "TB"];
  let value = bytes;
  let unitIndex = 0;

  while (value >= 1024 && unitIndex < units.length - 1) {
    value /= 1024;
    unitIndex += 1;
  }

  if (unitIndex === 0) {
    return `${Math.round(value)} ${units[unitIndex]}`;
  }

  return `${value.toFixed(1)} ${units[unitIndex]}`;
}

function formatDeployStatus(status: string): string {
  if (!status) {
    return "Unknown";
  }

  return status
    .split("_")
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(" ");
}

function formatRelativeExpiry(expiresTimestamp: number, now: number): string {
  if (!Number.isFinite(expiresTimestamp)) {
    return "unknown";
  }

  const diffMs = expiresTimestamp - now;
  const diffDays = Math.max(1, Math.ceil(Math.abs(diffMs) / 86_400_000));

  if (diffMs >= 0) {
    return `in ${diffDays}d`;
  }

  return `expired ${diffDays}d ago`;
}
