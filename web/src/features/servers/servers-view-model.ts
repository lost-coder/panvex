import type { TelemetryServerSummary } from "../../lib/api";

export type ServerTableFilter = "all" | "healthy" | "issues" | "stale";
export type ServerTableSortKey = "server" | "status" | "freshness" | "runtime" | "dc" | "upstreams" | "events";
export type ServerTableSortDir = "asc" | "desc";
export type ServerStatusTone = "online" | "degraded" | "offline";

export interface ServerTableRow {
  id: string;
  serverName: string;
  groupText: string;
  statusText: string;
  statusTone: ServerStatusTone;
  reasonText: string;
  freshnessText: string;
  freshnessRank: number;
  admissionText: string;
  runtimeText: string;
  dcSummaryText: string;
  upstreamSummaryText: string;
  eventText: string;
  isIssue: boolean;
  severity: number;
}

const numberFormatter = new Intl.NumberFormat("en-US");

export function buildServerTableRows(items: TelemetryServerSummary[]): ServerTableRow[] {
  return items.map((item) => {
    const summary = normalizeTelemetryServerSummary(item);
    const agent = summary.agent;
    const statusTone = resolveServerStatusTone(agent, summary.severity);
    const firstEvent = [...(agent.runtime?.recent_events ?? [])]
      .sort((left, right) => right.timestamp_unix - left.timestamp_unix || right.sequence - left.sequence)[0];

    return {
      id: agent.id,
      serverName: agent.node_name,
      groupText: agent.fleet_group_id || "Ungrouped",
      statusText: statusTone === "offline" ? "Offline" : statusTone === "degraded" ? "Attention" : "Healthy",
      statusTone,
      reasonText: summary.reason,
      freshnessText: humanizeFreshness(summary.runtime_freshness.state),
      freshnessRank: freshnessRank(summary.runtime_freshness.state),
      admissionText: agent.runtime?.accepting_new_connections ? "Open" : "Closed",
      runtimeText: `${humanizeToken(agent.runtime?.transport_mode || "unknown")} • ${numberFormatter.format(agent.runtime?.current_connections ?? 0)} conns`,
      dcSummaryText: agent.runtime?.dcs?.length
        ? `${Math.round(agent.runtime?.dc_coverage_pct ?? 0)}% across ${agent.runtime?.dcs.length ?? 0} DCs`
        : "No DC data",
      upstreamSummaryText: `${numberFormatter.format(agent.runtime?.healthy_upstreams ?? 0)} / ${numberFormatter.format(agent.runtime?.total_upstreams ?? 0)} healthy`,
      eventText: firstEvent ? firstEvent.context || humanizeToken(firstEvent.event_type) : "No recent events",
      isIssue: summary.severity !== "good" || summary.runtime_freshness.state !== "fresh",
      severity: summary.severity === "bad" ? 3 : summary.severity === "warn" ? 2 : 1,
    };
  });
}

export function buildServerFilterCounts(rows: ServerTableRow[]) {
  return {
    all: rows.length,
    healthy: rows.filter((row) => !row.isIssue).length,
    issues: rows.filter((row) => row.isIssue).length,
    stale: rows.filter((row) => row.freshnessRank >= 2).length,
  };
}

export function filterServerTableRows(rows: ServerTableRow[], options: { filter: ServerTableFilter; search: string }) {
  const normalizedSearch = options.search.trim().toLowerCase();

  return rows.filter((row) => {
    const matchesFilter =
      options.filter === "all" ||
      (options.filter === "healthy" && !row.isIssue) ||
      (options.filter === "issues" && row.isIssue) ||
      (options.filter === "stale" && row.freshnessRank >= 2);

    if (!matchesFilter) {
      return false;
    }

    if (!normalizedSearch) {
      return true;
    }

    return [row.serverName, row.groupText, row.reasonText, row.eventText].some((value) =>
      value.toLowerCase().includes(normalizedSearch)
    );
  });
}

export function sortServerTableRows(rows: ServerTableRow[], sortKey: ServerTableSortKey, sortDir: ServerTableSortDir) {
  const direction = sortDir === "asc" ? 1 : -1;

  return [...rows].sort((leftRow, rightRow) => {
    const comparison = compareByKey(leftRow, rightRow, sortKey);
    if (comparison !== 0) {
      return comparison * direction;
    }

    return leftRow.serverName.localeCompare(rightRow.serverName, "en", { sensitivity: "base" });
  });
}

export function paginateServerTableRows(rows: ServerTableRow[], page: number, pageSize: number) {
  const totalPages = Math.max(1, Math.ceil(rows.length / pageSize));
  const safePage = Math.min(Math.max(page, 1), totalPages);
  const startIndex = (safePage - 1) * pageSize;

  return {
    rows: rows.slice(startIndex, startIndex + pageSize),
    totalPages,
  };
}

function resolveServerStatusTone(
  agent: TelemetryServerSummary["agent"],
  severity: TelemetryServerSummary["severity"]
): ServerStatusTone {
  if (agent.presence_state === "offline") {
    return "offline";
  }
  if (severity !== "good") {
    return "degraded";
  }
  return "online";
}

function humanizeToken(value: string): string {
  return value
    .split(/[_-]+/g)
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(" ");
}

function humanizeFreshness(value: string): string {
  switch (value) {
    case "fresh":
      return "Fresh";
    case "stale":
      return "Stale";
    case "disabled":
      return "Disabled";
    case "unavailable":
      return "Unavailable";
    default:
      return "Pending";
  }
}

function freshnessRank(value: string): number {
  switch (value) {
    case "stale":
      return 2;
    case "unavailable":
    case "disabled":
      return 3;
    case "fresh":
      return 1;
    default:
      return 0;
  }
}

function compareByKey(leftRow: ServerTableRow, rightRow: ServerTableRow, sortKey: ServerTableSortKey) {
  switch (sortKey) {
    case "status":
      return leftRow.severity - rightRow.severity;
    case "freshness":
      return leftRow.freshnessRank - rightRow.freshnessRank;
    case "runtime":
      return leftRow.runtimeText.localeCompare(rightRow.runtimeText, "en", { sensitivity: "base" });
    case "dc":
      return leftRow.dcSummaryText.localeCompare(rightRow.dcSummaryText, "en", { sensitivity: "base" });
    case "upstreams":
      return leftRow.upstreamSummaryText.localeCompare(rightRow.upstreamSummaryText, "en", { sensitivity: "base" });
    case "events":
      return leftRow.eventText.localeCompare(rightRow.eventText, "en", { sensitivity: "base" });
    case "server":
    default:
      return leftRow.serverName.localeCompare(rightRow.serverName, "en", { sensitivity: "base" });
  }
}

function normalizeTelemetryServerSummary(input: TelemetryServerSummary | any): TelemetryServerSummary {
  if (input && "agent" in input) {
    return input;
  }

  const severity = input?.presence_state === "offline"
    ? "bad"
    : input?.presence_state === "degraded" || input?.runtime?.degraded
      ? "warn"
      : "good";

  return {
    agent: input,
    severity,
    reason: severity === "bad" ? "Agent heartbeat is offline" : severity === "warn" ? "Runtime is degraded" : "Node is ready",
    runtime_freshness: {
      state: "fresh",
      observed_at_unix: input?.last_seen_at ? Math.floor(Date.parse(input.last_seen_at) / 1000) : 0,
    },
    detail_boost: {
      active: false,
      expires_at_unix: 0,
      remaining_seconds: 0,
    },
  };
}
