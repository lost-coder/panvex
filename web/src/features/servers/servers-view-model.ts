import type { Agent } from "../../lib/api";

export type ServerTableFilter = "all" | "online" | "issues" | "offline";
export type ServerTableSortKey = "server" | "status" | "clients" | "cpu" | "memory" | "dc" | "traffic" | "uptime";
export type ServerTableSortDir = "asc" | "desc";
export type ServerStatusTone = "online" | "degraded" | "offline";
export type DcSegmentTone = "ok" | "partial" | "down";

export interface ServerTableRow {
  id: string;
  serverName: string;
  groupText: string;
  statusText: string;
  statusTone: ServerStatusTone;
  presenceState: string;
  isIssue: boolean;
  severity: number;
  clientsValue: number;
  clientsText: string;
  cpuText: string;
  memoryText: string;
  trafficText: string;
  uptimeText: string;
  dcAvailableCount: number;
  dcTotalCount: number;
  dcSummaryText: string;
  dcSegments: DcSegmentTone[];
}

const numberFormatter = new Intl.NumberFormat("en-US");

export function buildServerTableRows(agents: Agent[]): ServerTableRow[] {
  return agents.map((agent) => {
    const statusTone = resolveServerStatusTone(agent);
    const dcs = agent.runtime?.dcs ?? [];
    const dcSegments = dcs.map(resolveDcSegmentTone);
    const dcAvailableCount = dcs.filter((dc) => (dc.coverage_pct ?? 0) > 0).length;
    const dcTotalCount = dcs.length;
    const clientsValue = agent.runtime?.active_users ?? 0;

    return {
      id: agent.id,
      serverName: agent.node_name,
      groupText: agent.fleet_group_id || "Ungrouped",
      statusText: statusTone.charAt(0).toUpperCase() + statusTone.slice(1),
      statusTone,
      presenceState: agent.presence_state,
      isIssue: statusTone !== "online",
      severity: statusTone === "offline" ? 3 : statusTone === "degraded" ? 2 : 1,
      clientsValue,
      clientsText: statusTone === "offline" ? "—" : numberFormatter.format(clientsValue),
      cpuText: "—",
      memoryText: "—",
      trafficText: "—",
      uptimeText: formatUptime(agent.runtime?.uptime_seconds),
      dcAvailableCount,
      dcTotalCount,
      dcSummaryText: dcTotalCount > 0 ? `${dcAvailableCount}/${dcTotalCount}` : "—",
      dcSegments,
    };
  });
}

export function buildServerFilterCounts(rows: ServerTableRow[]) {
  return {
    all: rows.length,
    online: rows.filter((row) => !row.isIssue).length,
    issues: rows.filter((row) => row.isIssue).length,
    offline: rows.filter((row) => row.statusTone === "offline").length,
  };
}

export function filterServerTableRows(
  rows: ServerTableRow[],
  options: { filter: ServerTableFilter; search: string }
) {
  const normalizedSearch = options.search.trim().toLowerCase();

  return rows.filter((row) => {
    const matchesFilter =
      options.filter === "all" ||
      (options.filter === "online" && !row.isIssue) ||
      (options.filter === "issues" && row.isIssue) ||
      (options.filter === "offline" && row.statusTone === "offline");

    if (!matchesFilter) {
      return false;
    }

    if (!normalizedSearch) {
      return true;
    }

    return [
      row.serverName,
      row.groupText,
      row.statusText,
    ].some((value) => value.toLowerCase().includes(normalizedSearch));
  });
}

export function sortServerTableRows(
  rows: ServerTableRow[],
  sortKey: ServerTableSortKey,
  sortDir: ServerTableSortDir
) {
  const direction = sortDir === "asc" ? 1 : -1;

  return [...rows].sort((leftRow, rightRow) => {
    const comparison = compareByKey(leftRow, rightRow, sortKey);

    if (comparison !== 0) {
      return comparison * direction;
    }

    return leftRow.serverName.localeCompare(rightRow.serverName, "en", { sensitivity: "base" });
  });
}

export function paginateServerTableRows(
  rows: ServerTableRow[],
  page: number,
  pageSize: number
) {
  const totalPages = Math.max(1, Math.ceil(rows.length / pageSize));
  const safePage = Math.min(Math.max(page, 1), totalPages);
  const startIndex = (safePage - 1) * pageSize;

  return {
    rows: rows.slice(startIndex, startIndex + pageSize),
    totalPages,
  };
}

function resolveServerStatusTone(agent: Agent): ServerStatusTone {
  if (agent.presence_state === "offline") {
    return "offline";
  }

  if (agent.presence_state === "degraded" || agent.runtime?.degraded) {
    return "degraded";
  }

  return "online";
}

function resolveDcSegmentTone(dc: Agent["runtime"]["dcs"][number]): DcSegmentTone {
  if ((dc.coverage_pct ?? 0) >= 100) {
    return "ok";
  }

  if ((dc.coverage_pct ?? 0) > 0) {
    return "partial";
  }

  return "down";
}

function compareByKey(
  leftRow: ServerTableRow,
  rightRow: ServerTableRow,
  sortKey: ServerTableSortKey
) {
  switch (sortKey) {
    case "status":
      return leftRow.severity - rightRow.severity;
    case "clients":
      return leftRow.clientsValue - rightRow.clientsValue;
    case "dc":
      return (
        leftRow.dcAvailableCount - rightRow.dcAvailableCount ||
        leftRow.dcTotalCount - rightRow.dcTotalCount
      );
    case "cpu":
      return leftRow.cpuText.localeCompare(rightRow.cpuText, "en", { sensitivity: "base" });
    case "memory":
      return leftRow.memoryText.localeCompare(rightRow.memoryText, "en", { sensitivity: "base" });
    case "traffic":
      return leftRow.trafficText.localeCompare(rightRow.trafficText, "en", { sensitivity: "base" });
    case "uptime":
      return leftRow.uptimeText.localeCompare(rightRow.uptimeText, "en", { sensitivity: "base" });
    case "server":
    default:
      return leftRow.serverName.localeCompare(rightRow.serverName, "en", { sensitivity: "base" });
  }
}

function formatUptime(value: number | undefined): string {
  if (!Number.isFinite(value) || value === undefined || value <= 0) {
    return "—";
  }

  const totalSeconds = Math.floor(value);
  if (totalSeconds < 60) {
    return `${totalSeconds}s`;
  }

  const totalMinutes = Math.floor(totalSeconds / 60);
  if (totalMinutes < 60) {
    return `${totalMinutes}m`;
  }

  const totalHours = Math.floor(totalMinutes / 60);
  if (totalHours < 24) {
    const minutes = totalMinutes % 60;
    return minutes > 0 ? `${totalHours}h ${minutes}m` : `${totalHours}h`;
  }

  const days = Math.floor(totalHours / 24);
  const hours = totalHours % 24;
  return hours > 0 ? `${days}d ${hours}h` : `${days}d`;
}
