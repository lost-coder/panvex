import type { ServerDcData, ServerDetailPageProps } from "@/shared/api/types-pages/pages";

export type ServerEvent = ServerDetailPageProps["server"]["events"][number];

export type AlertItem = {
  severity: "crit" | "warn";
  message: string;
  source: string;
};

export type DCStripItem = {
  code: string;
  city: string;
  latency: number;
  load: number;
  status: "ok" | "warn" | "error";
};

export type TimelineEvent = {
  tsEpochSecs: number;
  kind: "ok" | "warn" | "error" | "info";
};

/** Coverage rollup over a server's DC list. */
export function computeCoverageStats(dcs: ServerDcData[]) {
  const minCoverage =
    dcs.length > 0 ? Math.min(...dcs.map((d) => d.coveragePct)) : 100;
  const avgCoverage =
    dcs.length > 0
      ? Math.round(dcs.reduce((s, d) => s + d.coveragePct, 0) / dcs.length)
      : 100;
  const dcOk = dcs.filter((d) => d.coveragePct >= 100).length;
  const dcWarn = dcs.filter((d) => d.coveragePct < 100 && d.coveragePct >= 70).length;
  const dcErr = dcs.filter((d) => d.coveragePct < 70).length;
  return { minCoverage, avgCoverage, dcOk, dcWarn, dcErr };
}

/** Bad-rate as a percentage; 0 when there are no connections at all. */
export function computeBadRate(connectionsBadTotal: number, connectionsTotal: number): number {
  return connectionsTotal > 0 ? (connectionsBadTotal / connectionsTotal) * 100 : 0;
}

/** Hero status sentence (e.g. "DEGRADED · 2 DCs offline"). */
export function statusSentence(
  status: "ok" | "warn" | "error",
  dcCount: number,
  dcWarn: number,
  dcErr: number,
): string {
  if (status === "error") return `DEGRADED · ${dcErr} DC${dcErr > 1 ? "s" : ""} offline`;
  if (status === "warn") return `STRAINED · ${dcWarn} DC${dcWarn > 1 ? "s" : ""} under coverage`;
  return `HEALTHY · all ${dcCount || 12} routes nominal`;
}

function dcStripStatus(coveragePct: number): DCStripItem["status"] {
  if (coveragePct < 70) return "error";
  if (coveragePct < 100) return "warn";
  return "ok";
}

/** DCScrollStrip projection from a sorted DC list. */
export function toDcStripItems(sortedDcs: ServerDcData[]): DCStripItem[] {
  return sortedDcs.map((dc) => ({
    code: `DC${dc.dc}`,
    city: `DC ${dc.dc}`,
    latency: dc.rttMs ?? 0,
    load: dc.load,
    status: dcStripStatus(dc.coveragePct),
  }));
}

/** Build the alert strip items from coverage gaps and degraded gates. */
export function computeAlertItems(
  sortedDcs: ServerDcData[],
  gates: ServerDetailPageProps["server"]["gates"],
  hasInitState: boolean,
): AlertItem[] {
  const alerts: AlertItem[] = [];
  if (!hasInitState) {
    sortedDcs
      .filter((dc) => dc.coveragePct < 100)
      .forEach((dc) => {
        alerts.push({
          severity: dc.coveragePct < 70 ? ("crit" as const) : ("warn" as const),
          message: `DC${dc.dc} coverage at ${dc.coveragePct}% (${dc.aliveWriters}/${dc.requiredWriters} writers)`,
          source: "dc-coverage",
        });
      });
  }
  if (gates.degraded) {
    alerts.unshift({
      severity: "crit" as const,
      message: "Server operating in degraded mode",
      source: "gates",
    });
  }
  return alerts;
}

function timelineEventKind(eventType: string): TimelineEvent["kind"] {
  if (/error|fail|down|offline/i.test(eventType)) return "error";
  if (/warn|degrad|slow/i.test(eventType)) return "warn";
  if (/ready|online|recover|connect/i.test(eventType)) return "ok";
  return "info";
}

/** Classify a server event into the timeline-pin tone. */
export function toTimelineEvents(events: ServerEvent[] | undefined): TimelineEvent[] {
  return (events ?? []).slice(0, 10).map((e) => ({
    tsEpochSecs: e.tsEpochSecs,
    kind: timelineEventKind(e.eventType),
  }));
}
