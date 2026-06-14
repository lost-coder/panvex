import { coverageStatus } from "@/ui/lib/status";
import type {
  ModeKind,
  ServerDcData,
  ServerDetailPageProps,
  ServerUpstreamSummaryData,
} from "@/shared/api/types-pages/pages";

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

/** Coverage rollup over a server's DC list (ME / Fallback modes only — Direct has no DCs). */
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

/**
 * Per-mode context for the hero status sentence. Direct-mode nodes do
 * not have DCs; their health story is driven by upstream health and the
 * 5-minute connect fail-rate. ME and Fallback modes still talk DC
 * coverage (Fallback uses direct DC connections under the hood but the
 * operator still thinks in DC terms there). me_down has no health
 * vocabulary of its own — only the dedicated MeDownHero matters; this
 * branch exists so the hero strip above the swap is still coherent.
 */
export type StatusContext =
  | { mode: "me" | "fallback"; dcCount: number; dcWarn: number; dcErr: number }
  | {
      mode: "direct";
      upstreamHealthy: number;
      upstreamTotal: number;
      failRatePct5m: number;
      failRateKnown: boolean;
    }
  | { mode: "me_down" };

function plural(n: number, suffix = "s"): string {
  return n === 1 ? "" : suffix;
}

function statusSentenceDirect(
  status: "ok" | "warn" | "error",
  ctx: Extract<StatusContext, { mode: "direct" }>,
): string {
  const { upstreamHealthy, upstreamTotal, failRatePct5m, failRateKnown } = ctx;
  const rate = Math.round(failRatePct5m);
  if (status === "error") {
    if (failRateKnown && failRatePct5m >= 50) {
      return `DEGRADED · upstream fail-rate ${rate}%`;
    }
    if (upstreamTotal > 0 && upstreamHealthy === 0) {
      return "DEGRADED · all upstreams down";
    }
    return "DEGRADED · upstream connectivity lost";
  }
  if (status === "warn") {
    if (failRateKnown && failRatePct5m >= 10) {
      return `STRAINED · upstream fail-rate ${rate}%`;
    }
    if (upstreamTotal === 0) {
      return "STRAINED · no upstreams configured";
    }
    if (upstreamHealthy < upstreamTotal) {
      return `STRAINED · ${upstreamHealthy}/${upstreamTotal} upstream${plural(upstreamTotal)} healthy`;
    }
    return "STRAINED · upstreams degraded";
  }
  if (upstreamTotal > 0) {
    return `HEALTHY · ${upstreamTotal} upstream${plural(upstreamTotal)} nominal`;
  }
  return "HEALTHY · direct relay";
}

function statusSentenceMeBased(
  status: "ok" | "warn" | "error",
  ctx: Extract<StatusContext, { mode: "me" | "fallback" }>,
): string {
  const { dcCount, dcWarn, dcErr } = ctx;
  if (status === "error") {
    return `DEGRADED · ${dcErr} DC${plural(dcErr)} offline`;
  }
  if (status === "warn") {
    return `STRAINED · ${dcWarn} DC${plural(dcWarn)} under coverage`;
  }
  return `HEALTHY · all ${dcCount || 12} routes nominal`;
}

/** Hero status sentence rendered in ServerHero / mobile subtitle. */
export function statusSentence(
  status: "ok" | "warn" | "error",
  ctx: StatusContext,
): string {
  if (ctx.mode === "me_down") {
    return status === "ok"
      ? "STANDBY · ME pool initializing"
      : "DEGRADED · ME pool unavailable";
  }
  if (ctx.mode === "direct") {
    return statusSentenceDirect(status, ctx);
  }
  return statusSentenceMeBased(status, ctx);
}

/** DCScrollStrip projection from a sorted DC list. */
export function toDcStripItems(sortedDcs: ServerDcData[]): DCStripItem[] {
  return sortedDcs.map((dc) => ({
    code: `DC${dc.dc}`,
    city: `DC ${dc.dc}`,
    latency: dc.rttMs ?? 0,
    load: dc.load,
    // coverageStatus centralizes the < 70 / < 100 coverage thresholds
    // (also drives coverageColor) — keep DC strip severity in lockstep.
    status: coverageStatus(dc.coveragePct),
  }));
}

export interface AlertItemsInput {
  mode: ModeKind;
  sortedDcs: ServerDcData[];
  gates: ServerDetailPageProps["server"]["gates"];
  hasInitState: boolean;
  upstreamSummary?: ServerUpstreamSummaryData | undefined;
}

function pushDirectUpstreamAlerts(
  alerts: AlertItem[],
  summary: ServerUpstreamSummaryData,
): void {
  if (summary.failRateKnown && summary.failRatePct5m >= 10) {
    const rate = Math.round(summary.failRatePct5m);
    alerts.push({
      severity: summary.failRatePct5m >= 50 ? "crit" : "warn",
      message: `Upstream connect fail-rate at ${rate}% (5m window)`,
      source: "upstream-fail-rate",
    });
  }
  if (summary.configuredTotal > 0 && summary.unhealthyTotal > 0) {
    alerts.push({
      severity: summary.healthyTotal === 0 ? "crit" : "warn",
      message: `${summary.unhealthyTotal}/${summary.configuredTotal} upstream${plural(summary.configuredTotal)} unhealthy`,
      source: "upstream-health",
    });
  }
  if (summary.configuredTotal === 0) {
    alerts.push({
      severity: "warn",
      message: "No upstreams configured",
      source: "upstream-config",
    });
  }
}

/**
 * Build the alert strip items. Mode-aware: ME/Fallback list per-DC
 * coverage gaps; Direct lists upstream health and connect fail-rate.
 * gates.degraded is only meaningful in ME/Fallback after the backend
 * normalisation (see normalizeAgentRuntime), but we still gate the
 * message here so a stale snapshot doesn't slip through.
 */
export function computeAlertItems(input: AlertItemsInput): AlertItem[] {
  const { mode, sortedDcs, gates, hasInitState, upstreamSummary } = input;
  const alerts: AlertItem[] = [];

  if (!hasInitState) {
    if (mode === "me" || mode === "fallback") {
      sortedDcs
        .filter((dc) => dc.coveragePct < 100)
        .forEach((dc) => {
          alerts.push({
            severity: dc.coveragePct < 70 ? "crit" : "warn",
            message: `DC${dc.dc} coverage at ${dc.coveragePct}% (${dc.aliveWriters}/${dc.requiredWriters} writers)`,
            source: "dc-coverage",
          });
        });
    } else if (mode === "direct" && upstreamSummary) {
      pushDirectUpstreamAlerts(alerts, upstreamSummary);
    }
  }

  if (gates.degraded && (mode === "me" || mode === "fallback")) {
    alerts.unshift({
      severity: "crit",
      message: "ME runtime is degraded",
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
