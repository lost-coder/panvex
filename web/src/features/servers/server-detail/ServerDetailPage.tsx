import { useState } from "react";
import { ChevronRight } from "lucide-react";

import {
  AlertStrip,
  Breadcrumbs,
  FieldLabel,
  InitCard,
  MonoValue,
  PageHeader,
  SectionHeader,
  Sheet,
  SheetBody,
  SheetContent,
  SheetHeader,
  SheetTitle,
  StatusBeacon,
  SwipeTabView,
  cn,
  coverageColor,
  formatUptime,
} from "@/ui";
import { DCScrollStrip } from "@/features/servers/ui/DCScrollStrip";
import { AgentConnectionSection } from "@/features/servers/ui/AgentConnectionSection";
import { MetricsChartSection } from "@/features/dashboard/ui/MetricsChartSection";
import type { MetricsPoint } from "@/features/dashboard/ui/internal/MetricsChartSectionInner";
import type { ServerDetailPageProps, ServerDcData } from "@/shared/api/types-pages/pages";

import { useRelativeTime } from "./useRelativeTime";
import { ServerActionsDropdown } from "./ServerActionsDropdown";
import { PulseGrid } from "./components/PulseGrid";
import { ConnectionsTab } from "./tabs/ConnectionsTab";
import { MePoolTab } from "./tabs/MePoolTab";
import { UpstreamsTab } from "./tabs/UpstreamsTab";
import { EventsTab } from "./tabs/EventsTab";

const noop = () => {};

// ─── Health radar (12 DCs as a circular dial) ─────────────────────────
// A 12-segment pie-ring coloured by DC status plus a hub that reports
// fleet-average coverage. Clicking a segment opens the DC detail sheet.
function HealthRadar({
  dcs,
  onSelect,
}: {
  dcs: ServerDcData[];
  onSelect: (dc: ServerDcData) => void;
}) {
  const size = 240;
  const cx = size / 2;
  const cy = size / 2;
  const outer = size / 2 - 10;
  const inner = outer - 30;
  const gap = 2;

  // Order DCs by their dc number so the dial layout is stable across
  // renders. MTProto exposes positive + negative DC ids (e.g. -1..-6 for
  // media mirrors and 1..6 for the primary DCs); sort puts negatives
  // first, giving a consistent clockwise walk. No padding / slot-filling
  // is done — we render as many arcs as `dcs` contains, which previously
  // caused duplicates when a find-by-dc-number fallback substituted
  // `dcs[i]` with the wrong record.
  const ordered = [...dcs].sort((a, b) => a.dc - b.dc);
  const segCount = Math.max(1, ordered.length);
  const each = 360 / segCount;

  const arcPath = (i: number) => {
    const startA = -90 + i * each + gap / 2;
    const endA = -90 + (i + 1) * each - gap / 2;
    const rad = (a: number) => (a * Math.PI) / 180;
    const p = (r: number, a: number) => [cx + r * Math.cos(rad(a)), cy + r * Math.sin(rad(a))];
    const [x1, y1] = p(outer, startA);
    const [x2, y2] = p(outer, endA);
    const [x3, y3] = p(inner, endA);
    const [x4, y4] = p(inner, startA);
    return `M ${x1} ${y1} A ${outer} ${outer} 0 0 1 ${x2} ${y2} L ${x3} ${y3} A ${inner} ${inner} 0 0 0 ${x4} ${y4} Z`;
  };
  const labelPos = (i: number) => {
    const a = ((-90 + (i + 0.5) * each) * Math.PI) / 180;
    const r = (outer + inner) / 2;
    return [cx + r * Math.cos(a), cy + r * Math.sin(a)] as const;
  };

  const statusOf = (d: ServerDcData): "ok" | "warn" | "error" =>
    d.coveragePct < 70 ? "error" : d.coveragePct < 100 ? "warn" : "ok";
  const fillOf = (d: ServerDcData): string => {
    const s = statusOf(d);
    return s === "error"
      ? "var(--color-status-error)"
      : s === "warn"
        ? "var(--color-status-warn)"
        : "var(--color-status-ok)";
  };

  const total = ordered.length || 1;
  const avgPct = Math.round(
    ordered.reduce((sum, d) => sum + (d.coveragePct ?? 0), 0) / total,
  );
  const okCount = ordered.filter((d) => statusOf(d) === "ok").length;

  return (
    <div className="flex items-center justify-center">
      <svg viewBox={`0 0 ${size} ${size}`} width="100%" style={{ maxWidth: size }}>
        <circle
          cx={cx}
          cy={cy}
          r={outer + 3}
          fill="none"
          stroke="var(--color-divider)"
          strokeDasharray="1 3"
        />
        {ordered.map((d, i) => (
          <g key={d.dc} onClick={() => onSelect(d)} style={{ cursor: "pointer" }}>
            <path d={arcPath(i)} fill={fillOf(d)} opacity={statusOf(d) === "ok" ? 0.85 : 1} />
            {/* Mono numeral centred inside the arc. Keeps negative DC
                ids like "-4" legible next to positives. */}
            <text
              x={labelPos(i)[0]}
              y={labelPos(i)[1]}
              textAnchor="middle"
              dominantBaseline="middle"
              fontSize="9"
              fontFamily="JetBrains Mono, monospace"
              fontWeight={700}
              fill="rgba(11,13,18,0.9)"
              style={{ pointerEvents: "none", userSelect: "none" }}
            >
              {d.dc}
            </text>
          </g>
        ))}
        <circle
          cx={cx}
          cy={cy}
          r={inner - 8}
          fill="var(--color-bg)"
          stroke="var(--color-divider)"
        />
        <text
          x={cx}
          y={cy - 4}
          textAnchor="middle"
          fontSize="24"
          fontFamily="JetBrains Mono, monospace"
          fontWeight={600}
          fill="var(--color-fg)"
        >
          {avgPct}%
        </text>
        <text
          x={cx}
          y={cy + 12}
          textAnchor="middle"
          fontSize="9"
          fontFamily="JetBrains Mono, monospace"
          fill="var(--color-fg-muted)"
          letterSpacing="1"
        >
          {okCount}/{total} NOMINAL
        </text>
      </svg>
    </div>
  );
}

// ─── Timeline strip (two sparklines + event pins) ─────────────────────
// Plots connections + bad-rate over the available metric window with
// dashed vertical pins where recent events land. Falls back to the
// MetricsChartSection when the backend hasn't supplied series yet.
function TimelineStrip({
  metricsPoints,
  events,
}: {
  metricsPoints: MetricsPoint[];
  events: { tsEpochSecs: number; kind: "ok" | "warn" | "error" | "info" }[];
}) {
  if (metricsPoints.length < 2) {
    return (
      <div className="flex items-center justify-center h-[140px] text-xs font-mono text-fg-muted">
        Gathering telemetry…
      </div>
    );
  }
  // MetricsPoint stores the timestamp as an ISO string. Project connections
  // and CPU to numeric arrays for the two overlaid sparklines; bad-rate
  // isn't carried in the metrics feed so CPU stands in as the secondary
  // signal (legend below reflects that).
  const conn = metricsPoints.map((p) => p.connectionsAvg ?? 0);
  const cpu = metricsPoints.map((p) => p.cpuAvg ?? 0);

  const tsOf = (p: MetricsPoint) => Math.floor(new Date(p.t).getTime() / 1000);
  const tMin = tsOf(metricsPoints[0]!);
  const tMax = tsOf(metricsPoints[metricsPoints.length - 1]!);
  const range = Math.max(1, tMax - tMin);

  // Single responsive SVG — one viewBox, preserveAspectRatio="none",
  // parent sets `w-full` so the chart always fills the column. Two
  // sparklines, three gridlines, event pins share the same coordinate
  // space and scale together.
  const w = 1000;
  const h = 140;
  const pad = 14;
  const innerW = w - pad * 2;
  const innerH = h - pad * 2;

  const normalise = (data: number[]): string => {
    if (data.length === 0) return "";
    const min = Math.min(...data);
    const max = Math.max(...data);
    const r = max - min || 1;
    return data
      .map((v, i) => {
        const x = pad + (i / Math.max(1, data.length - 1)) * innerW;
        const y = pad + innerH - ((v - min) / r) * innerH;
        return `${x.toFixed(1)},${y.toFixed(1)}`;
      })
      .join(" ");
  };

  return (
    <div className="flex flex-col gap-1 w-full">
      <svg
        viewBox={`0 0 ${w} ${h}`}
        preserveAspectRatio="none"
        className="w-full h-[140px] block"
      >
        {[0.25, 0.5, 0.75].map((t) => (
          <line
            key={t}
            x1={pad}
            x2={w - pad}
            y1={pad + innerH * t}
            y2={pad + innerH * t}
            stroke="var(--color-divider)"
            strokeDasharray="2 4"
            vectorEffect="non-scaling-stroke"
          />
        ))}
        {/* Event pins: dashed vertical line + top-dot. */}
        {events.map((e, idx) => {
          const pct = (e.tsEpochSecs - tMin) / range;
          if (pct < 0 || pct > 1) return null;
          const x = pad + pct * innerW;
          const color =
            e.kind === "error"
              ? "var(--color-status-error)"
              : e.kind === "warn"
                ? "var(--color-status-warn)"
                : "var(--color-fg-muted)";
          return (
            <g key={idx}>
              <line
                x1={x}
                x2={x}
                y1={pad - 4}
                y2={h - pad}
                stroke={color}
                strokeDasharray="2 3"
                opacity={0.6}
                vectorEffect="non-scaling-stroke"
              />
              <circle cx={x} cy={pad - 2} r={3} fill={color} />
            </g>
          );
        })}
        {/* Sparklines. `non-scaling-stroke` keeps the line 1.5px crisp
            regardless of how wide the SVG stretches. */}
        <polyline
          points={normalise(conn)}
          fill="none"
          stroke="var(--color-accent)"
          strokeWidth={1.5}
          strokeLinejoin="round"
          strokeLinecap="round"
          vectorEffect="non-scaling-stroke"
        />
        <polyline
          points={normalise(cpu)}
          fill="none"
          stroke="var(--color-status-warn)"
          strokeWidth={1.5}
          strokeLinejoin="round"
          strokeLinecap="round"
          vectorEffect="non-scaling-stroke"
        />
      </svg>
      <div className="flex items-center justify-between text-[10px] font-mono text-fg-muted">
        <div className="flex gap-3">
          <span>
            <span style={{ color: "var(--color-accent)" }}>●</span> connections
          </span>
          <span>
            <span style={{ color: "var(--color-status-warn)" }}>●</span> cpu
          </span>
        </div>
        <span>{events.length} events</span>
      </div>
    </div>
  );
}

// ─── 12-DC grid of tiles (problem-first ordering) ─────────────────────
function DcTile({ dc, onClick }: { dc: ServerDcData; onClick: () => void }) {
  const status: "ok" | "warn" | "error" =
    dc.coveragePct < 70 ? "error" : dc.coveragePct < 100 ? "warn" : "ok";
  const toneBorder =
    status === "error"
      ? "border-status-error/50"
      : status === "warn"
        ? "border-status-warn/40"
        : "border-divider";
  const toneBar =
    status === "error"
      ? "bg-status-error"
      : status === "warn"
        ? "bg-status-warn"
        : "bg-status-ok/80";
  const toneText =
    status === "error"
      ? "text-status-error"
      : status === "warn"
        ? "text-status-warn"
        : "text-fg";
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "text-left rounded-xs bg-bg-card border px-3 py-2.5 flex flex-col gap-2 hover:bg-bg-hover transition-colors",
        toneBorder,
      )}
    >
      <div className="flex items-center justify-between">
        <span className="text-xs font-mono font-semibold text-fg">DC{dc.dc}</span>
        <span className={cn("h-1.5 w-1.5 rounded-full", toneBar)} />
      </div>
      <div className="flex items-baseline gap-1">
        <span className={cn("text-lg font-mono font-semibold tabular-nums", toneText)}>
          {dc.coveragePct}
        </span>
        <span className="text-xs font-mono text-fg-muted">%</span>
      </div>
      <div className="h-1 w-full rounded-full bg-border overflow-hidden">
        <div className={cn("h-full rounded-full", toneBar)} style={{ width: `${dc.coveragePct}%` }} />
      </div>
      <div className="flex items-center justify-between text-[10px] font-mono text-fg-muted">
        <span>
          w{" "}
          <span className={cn(dc.aliveWriters < dc.requiredWriters ? "text-status-warn" : "text-fg")}>
            {dc.aliveWriters}/{dc.requiredWriters}
          </span>
        </span>
        <span>
          rtt{" "}
          <span className={cn((dc.rttMs ?? 0) > 300 ? "text-status-error" : (dc.rttMs ?? 0) > 200 ? "text-status-warn" : "text-fg")}>
            {dc.rttMs ?? "—"}
          </span>
        </span>
        <span>load {Math.round(dc.load)}%</span>
      </div>
    </button>
  );
}

function DcTiles({
  dcs,
  onSelect,
}: {
  dcs: ServerDcData[];
  onSelect: (dc: ServerDcData) => void;
}) {
  return (
    <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-6 gap-2">
      {dcs.map((dc) => (
        <DcTile key={dc.dc} dc={dc} onClick={() => onSelect(dc)} />
      ))}
    </div>
  );
}

// ─── Gates panel ──────────────────────────────────────────────────────
function GateRow({
  label,
  on,
  alertWhenOn,
  neutralWhenOn,
}: {
  label: string;
  on: boolean;
  alertWhenOn?: boolean;
  neutralWhenOn?: boolean;
}) {
  const tone: "ok" | "warn" | "error" | "default" = alertWhenOn
    ? on
      ? "warn"
      : "ok"
    : neutralWhenOn
      ? "default"
      : on
        ? "ok"
        : "error";
  const dot =
    tone === "ok"
      ? "bg-status-ok"
      : tone === "warn"
        ? "bg-status-warn"
        : tone === "error"
          ? "bg-status-error"
          : "bg-fg-muted/60";
  // Handoff-style "dashed divider row" inside the Gates column — lighter
  // than the solid dividers used in the Upstreams column so the two
  // columns read as different content types at a glance.
  return (
    <div className="flex items-center justify-between gap-3 py-2 border-b border-dashed border-divider last:border-b-0">
      <div className="flex items-center gap-2 min-w-0">
        <span className={cn("h-1.5 w-1.5 rounded-full shrink-0", dot)} />
        <span className="text-xs text-fg truncate">{label}</span>
      </div>
      <span className="text-[10px] font-mono font-semibold uppercase tracking-wider text-fg-muted">
        {on ? "on" : "off"}
      </span>
    </div>
  );
}

function GatesPanel({ gates }: { gates: ServerDetailPageProps["server"]["gates"] }) {
  return (
    <div className="flex flex-col">
      <GateRow label="Accepting connections" on={gates.acceptingNewConnections} />
      <GateRow label="ME runtime ready" on={gates.meRuntimeReady} />
      <GateRow label="Middle proxy" on={gates.useMiddleProxy} neutralWhenOn />
      <GateRow label="ME → DC fallback" on={gates.me2dcFallbackEnabled} neutralWhenOn />
      <GateRow label="Reroute active" on={gates.rerouteActive} alertWhenOn />
      <GateRow label="Read-only mode" on={gates.readOnly} alertWhenOn />
      <GateRow label="Degraded" on={gates.degraded} alertWhenOn />
    </div>
  );
}

// ─── Upstreams list ───────────────────────────────────────────────────
function UpstreamsList({
  upstreams,
}: {
  upstreams: ServerDetailPageProps["server"]["upstreams"];
}) {
  if (upstreams.length === 0) {
    return (
      <div className="text-xs font-mono text-fg-muted px-3 py-6 text-center">No upstreams reported.</div>
    );
  }
  return (
    <div className="flex flex-col gap-1.5">
      {upstreams.map((u) => (
        <div
          key={u.upstreamId}
          // Darker solid panel rows — opposite visual treatment from the
          // dashed Gates rows next to them. Reads as "these are distinct
          // things you can drill into" versus "these are state flags".
          className="flex items-center gap-2 px-3 py-2 rounded-xs bg-bg border border-divider"
        >
          <span
            className={cn(
              "h-1.5 w-1.5 rounded-full shrink-0",
              u.healthy ? "bg-status-ok" : "bg-status-error",
            )}
          />
          <span className="text-xs font-mono text-fg truncate">{u.address}</span>
          <span className="ml-auto text-[10px] font-mono text-fg-muted tabular-nums shrink-0">
            {u.effectiveLatencyMs != null ? `${u.effectiveLatencyMs.toFixed(0)}ms` : "—"}
          </span>
          <span className="text-[10px] font-mono text-fg-muted tabular-nums shrink-0">
            {u.routeKind}
          </span>
        </div>
      ))}
    </div>
  );
}

// ─── Collapsible fold ─────────────────────────────────────────────────
function Fold({
  title,
  rightHint,
  defaultOpen = true,
  children,
}: {
  title: string;
  rightHint?: string;
  defaultOpen?: boolean;
  children: React.ReactNode;
}) {
  const [open, setOpen] = useState(defaultOpen);
  return (
    <section className="rounded-xs bg-bg-card border border-border overflow-hidden">
      <button
        type="button"
        onClick={() => setOpen(!open)}
        aria-expanded={open}
        className="w-full flex items-center gap-3 px-4 py-3 text-left hover:bg-bg-hover transition-colors"
      >
        <ChevronRight
          className={cn("size-4 transition-transform", open && "rotate-90")}
          aria-hidden="true"
        />
        <span className="text-sm font-semibold text-fg">{title}</span>
        {rightHint && (
          <span className="ml-auto text-[11px] font-mono text-fg-muted">{rightHint}</span>
        )}
      </button>
      {open && <div className="px-4 py-4 border-t border-border">{children}</div>}
    </section>
  );
}

// ─── Main page ────────────────────────────────────────────────────────
export function ServerDetailPage({
  server,
  onBack,
  onReload,
  onBoostDetail,
  initState,
  lastUpdatedAt,
  agentConnection,
  onAllowReEnrollment,
  onRevokeGrant,
  onRename,
  onDeregister,
  metricsChart,
}: ServerDetailPageProps) {
  const { label: relativeTime, stale: relativeTimeStale } = useRelativeTime(lastUpdatedAt);
  const { systemInfo, gates, connections, summary, dcs } = server;

  const sortedDcs = [...dcs].sort((a, b) => a.coveragePct - b.coveragePct);
  const [selectedDc, setSelectedDc] = useState<ServerDcData | null>(null);
  const [renameOpen, setRenameOpen] = useState(false);
  const [renameValue, setRenameValue] = useState(server.name);
  const [deregisterOpen, setDeregisterOpen] = useState(false);

  const minCoverage =
    sortedDcs.length > 0 ? Math.min(...sortedDcs.map((d) => d.coveragePct)) : 100;
  const avgCoverage =
    sortedDcs.length > 0
      ? Math.round(sortedDcs.reduce((s, d) => s + d.coveragePct, 0) / sortedDcs.length)
      : 100;
  const dcOk = sortedDcs.filter((d) => d.coveragePct >= 100).length;
  const dcWarn = sortedDcs.filter((d) => d.coveragePct < 100 && d.coveragePct >= 70).length;
  const dcErr = sortedDcs.filter((d) => d.coveragePct < 70).length;
  const badRate =
    summary.connectionsTotal > 0
      ? (summary.connectionsBadTotal / summary.connectionsTotal) * 100
      : 0;

  const pulseWord =
    server.status === "error"
      ? `DEGRADED · ${dcErr} DC${dcErr > 1 ? "s" : ""} offline`
      : server.status === "warn"
        ? `STRAINED · ${dcWarn} DC${dcWarn > 1 ? "s" : ""} under coverage`
        : `HEALTHY · all ${sortedDcs.length || 12} routes nominal`;

  const dcItems = sortedDcs.map((dc) => ({
    code: `DC${dc.dc}`,
    city: `DC ${dc.dc}`,
    latency: dc.rttMs ?? 0,
    load: dc.load,
    status:
      dc.coveragePct < 70
        ? ("error" as const)
        : dc.coveragePct < 100
          ? ("warn" as const)
          : ("ok" as const),
  }));

  const alertItems: { severity: "crit" | "warn"; message: string; source: string }[] = [];
  if (!initState) {
    sortedDcs
      .filter((dc) => dc.coveragePct < 100)
      .forEach((dc) => {
        alertItems.push({
          severity: dc.coveragePct < 70 ? ("crit" as const) : ("warn" as const),
          message: `DC${dc.dc} coverage at ${dc.coveragePct}% (${dc.aliveWriters}/${dc.requiredWriters} writers)`,
          source: "dc-coverage",
        });
      });
  }
  if (gates.degraded) {
    alertItems.unshift({
      severity: "crit" as const,
      message: "Server operating in degraded mode",
      source: "gates",
    });
  }

  // Mobile subtitle now carries the same status sentence the desktop
  // hero uses, plus compact meta (version + uptime + optional config
  // reload count). IP moves out of the subtitle since the GaugeStrip /
  // pulse row underneath shows the agent's actual runtime.
  const subtitle = [
    pulseWord.toLowerCase(),
    `v${systemInfo.version}`,
    `up ${formatUptime(systemInfo.uptimeSeconds)}`,
    systemInfo.configReloadCount > 0 ? `${systemInfo.configReloadCount} reloads` : null,
  ]
    .filter(Boolean)
    .join(" · ");

  const connectionsContent = <ConnectionsTab server={server} />;
  const mePoolContent = <MePoolTab server={server} />;
  const upstreamsContent = <UpstreamsTab server={server} />;
  const eventsContent = <EventsTab server={server} />;

  // Mobile tab content for gates + upstreams — mirrors the desktop
  // "one card, two columns" composition but stacks vertically so it
  // reads well in a narrow swipe pane.
  const gatesUpstreamsContent = (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col gap-2">
        <span className="text-sm font-semibold text-fg">Gates</span>
        <GatesPanel gates={gates} />
      </div>
      <div className="flex flex-col gap-2">
        <div className="flex items-center justify-between">
          <span className="text-sm font-semibold text-fg">Upstreams</span>
          <span className="text-[10px] font-mono text-fg-muted">
            {server.upstreams.length} peer{server.upstreams.length === 1 ? "" : "s"}
          </span>
        </div>
        <UpstreamsList upstreams={server.upstreams} />
      </div>
    </div>
  );

  // Diagnostics tab removed — the two fields that lived there (version
  // and config reload count) now ride in PageHeader subtitle.
  // Upstreams tab replaced by "Gates & Upstreams" so the mobile flow
  // mirrors the desktop card without a per-tab split.
  const mobileTabs = [
    { id: "connections", label: "Top clients", content: connectionsContent },
    { id: "me-pool", label: "ME Pool", content: mePoolContent },
    { id: "gates", label: "Gates & Upstreams", content: gatesUpstreamsContent },
    { id: "events", label: "Events", content: eventsContent },
  ];
  // Silence the unused-import warning for the now-desktop-only Upstreams
  // tab component — we still need it for the legacy type shape until the
  // mobile swipe tabs finish migrating.
  void upstreamsContent;

  // Desktop timeline strip inputs — pulled from metricsChart when the
  // backend supplied one. The sparkline renders connections + bad rate
  // with recent-event pins underneath.
  const timelinePoints = metricsChart?.points ?? [];
  const timelineEvents = (server.events ?? []).slice(0, 10).map((e) => ({
    tsEpochSecs: e.tsEpochSecs,
    kind:
      /error|fail|down|offline/i.test(e.eventType)
        ? ("error" as const)
        : /warn|degrad|slow/i.test(e.eventType)
          ? ("warn" as const)
          : /ready|online|recover|connect/i.test(e.eventType)
            ? ("ok" as const)
            : ("info" as const),
  }));

  return (
    <>
      <div className="px-4 md:px-8 pt-3 pb-3">
        <Breadcrumbs items={[{ label: "Servers", onClick: onBack }, { label: server.name }]} />
      </div>

      {/* Desktop: no PageHeader — the hero pulse-bar inside the page body
          carries name, status and actions, so a separate header would
          just duplicate the title. Mobile still gets PageHeader so the
          sticky app-bar stays populated. */}
      <div className="md:hidden">
        <PageHeader
          title={server.name}
          subtitle={subtitle}
          trailing={
            <div className="flex items-center gap-3">
              {relativeTime && (
                <span
                  className={cn(
                    "text-[10px] font-mono tabular-nums inline-flex items-center gap-1 rounded-full px-2 py-0.5 border transition-colors duration-500",
                    relativeTimeStale
                      ? "bg-status-warn/10 border-status-warn/15 text-status-warn"
                      : "bg-status-ok/10 border-status-ok/15 text-fg-muted",
                  )}
                >
                  <span className="text-[11px] animate-spin" style={{ animationDuration: "3s" }}>
                    ↻
                  </span>
                  {relativeTime}
                </span>
              )}
              <StatusBeacon status={server.status} size="xs" />
              <ServerActionsDropdown
                onReload={onReload}
                onBoostDetail={onBoostDetail}
                onRename={
                  onRename
                    ? () => {
                        setRenameValue(server.name);
                        setRenameOpen(true);
                      }
                    : undefined
                }
                onDeregister={onDeregister ? () => setDeregisterOpen(true) : undefined}
              />
            </div>
          }
        />
      </div>

      {/* Desktop hero as a full-bleed band: the `border-y` stretches to
          the viewport edges (no parent padding), while the inner content
          picks up the normal page gutters via `px-4 md:px-8`. Hidden on
          mobile — phones use PageHeader instead. */}
      <section className="hidden md:block border-y border-divider">
        <div className="px-4 md:px-8 py-4 flex flex-wrap items-center gap-x-4 gap-y-2">
          <StatusBeacon status={server.status} size="sm" />
          <h2 className="font-mono text-lg font-semibold text-fg truncate">{server.name}</h2>
          <span className="text-fg-faint">/</span>
          <span
            className={cn(
              "font-mono text-xs uppercase tracking-wider",
              server.status === "error"
                ? "text-status-error"
                : server.status === "warn"
                  ? "text-status-warn"
                  : "text-status-ok",
            )}
          >
            {pulseWord}
          </span>
          <div className="ml-auto flex items-center gap-2 flex-wrap justify-end">
            {server.ip && (
              <span className="font-mono text-[11px] text-fg-muted px-2 py-0.5 rounded-xs border border-divider bg-bg">
                {server.ip}
              </span>
            )}
            <span className="font-mono text-[11px] text-fg-muted px-2 py-0.5 rounded-xs border border-divider bg-bg">
              v{systemInfo.version}
            </span>
            <span className="font-mono text-[11px] text-fg-muted px-2 py-0.5 rounded-xs border border-divider bg-bg">
              up {formatUptime(systemInfo.uptimeSeconds)}
            </span>
            {systemInfo.configReloadCount > 0 && (
              <span className="font-mono text-[11px] text-fg-muted px-2 py-0.5 rounded-xs border border-divider bg-bg">
                reloads: {systemInfo.configReloadCount}
              </span>
            )}
            {relativeTime && (
              <span
                className={cn(
                  "text-[10px] font-mono tabular-nums inline-flex items-center gap-1 rounded-full px-2 py-0.5 border transition-colors duration-500",
                  relativeTimeStale
                    ? "bg-status-warn/10 border-status-warn/15 text-status-warn"
                    : "bg-status-ok/10 border-status-ok/15 text-fg-muted",
                )}
              >
                <span className="text-[11px] animate-spin" style={{ animationDuration: "3s" }}>
                  ↻
                </span>
                {relativeTime}
              </span>
            )}
            <ServerActionsDropdown
              onReload={onReload}
              onBoostDetail={onBoostDetail}
              onRename={
                onRename
                  ? () => {
                      setRenameValue(server.name);
                      setRenameOpen(true);
                    }
                  : undefined
              }
              onDeregister={onDeregister ? () => setDeregisterOpen(true) : undefined}
            />
          </div>
        </div>
      </section>

      <div className="px-4 md:px-8 flex flex-col gap-6 pb-8 pt-6">
        {/* Mobile layout preserved — operators on phones get the compact
            KPI + DC scroll strip + swipe tabs they're used to. */}
        <div className="md:hidden flex flex-col gap-4">
          {/* Gates moved into the "Gates & Upstreams" swipe tab; the
              badge row would have duplicated that signal. */}
          {initState && <InitCard {...initState} />}
          {/* Pulse tickers in a 2×2 grid with vertical + horizontal
              dividers between every cell. */}
          <PulseGrid
            variant="mobile"
            items={[
              {
                label: "Connections",
                value: connections.current.toLocaleString(),
                hint: `${connections.currentMe.toLocaleString()} ME · ${connections.currentDirect.toLocaleString()} direct`,
              },
              {
                label: "Active users",
                value: connections.activeUsers.toLocaleString(),
                hint: `of ${summary.configuredUsers.toLocaleString()}`,
              },
              {
                label: "Bad rate",
                value: `${badRate.toFixed(2)}%`,
                hint: `${summary.connectionsBadTotal.toLocaleString()} bad`,
                tone: badRate > 5 ? "error" : badRate > 1 ? "warn" : "default",
              },
              {
                label: "DC coverage",
                value: avgCoverage,
                unit: "%",
                hint: `min ${minCoverage}% · ${dcOk}/${dcWarn}/${dcErr}`,
                tone: avgCoverage < 95 ? "error" : avgCoverage < 100 ? "warn" : "ok",
              },
            ]}
          />
          {alertItems.length > 0 && <AlertStrip alerts={alertItems} />}
          {metricsChart && metricsChart.points.length > 0 && (
            <MetricsChartSection
              points={metricsChart.points}
              resolution={metricsChart.resolution}
              timeRange={metricsChart.timeRange}
              onTimeRangeChange={metricsChart.onTimeRangeChange}
            />
          )}
          <div>
            <SectionHeader title="Data Centers" badge={sortedDcs.length} />
            <DCScrollStrip
              items={dcItems}
              onSelect={(code) => {
                const dcNum = parseInt(code.replace("DC", ""), 10);
                setSelectedDc(sortedDcs.find((d) => d.dc === dcNum) ?? null);
              }}
            />
          </div>
          <SwipeTabView tabs={mobileTabs} />
        </div>

        {/* Desktop: handoff-style vertical story without tabs. */}
        <div className="hidden md:flex flex-col gap-6">
          {initState && <InitCard {...initState} />}

          {/* Pulse row — 4 metrics as tickers in a 4-col ribbon.
              Hint strings fold in the context that used to live inside
              the separate "Connections detail" fold (routing split,
              lifetime totals, configured users). */}
          <PulseGrid
            variant="desktop"
            items={[
              {
                label: "Connections",
                value: connections.current.toLocaleString(),
                hint: `${connections.currentMe.toLocaleString()} ME · ${connections.currentDirect.toLocaleString()} direct · total ${summary.connectionsTotal.toLocaleString()}`,
              },
              {
                label: "Active users",
                value: connections.activeUsers.toLocaleString(),
                hint: `of ${summary.configuredUsers.toLocaleString()} configured`,
              },
              {
                label: "Bad rate",
                value: `${badRate.toFixed(2)}%`,
                hint: `${summary.connectionsBadTotal.toLocaleString()} bad / ${summary.connectionsTotal.toLocaleString()} total`,
                tone: badRate > 5 ? "error" : badRate > 1 ? "warn" : "default",
              },
              {
                label: "DC coverage",
                value: avgCoverage,
                unit: "%",
                hint: `min ${minCoverage}% · ${dcOk} ok · ${dcWarn} warn · ${dcErr} err`,
                tone: avgCoverage < 95 ? "error" : avgCoverage < 100 ? "warn" : "ok",
              },
            ]}
          />

          {/* Health radar + live telemetry share one card with NO
              vertical divider between them — instead each column has a
              label row underlined by a horizontal border, which mirrors
              the handoff where the two contents read as one continuous
              panel split only by their headings. */}
          <section className="rounded-xs bg-bg-card border border-border p-4">
            <div className="grid grid-cols-[260px_minmax(0,1fr)] gap-6 items-start">
              <div className="flex flex-col gap-3">
                <div className="flex items-center justify-between gap-3 pb-2 border-b border-divider">
                  <span className="text-[10px] font-mono uppercase tracking-wider text-fg-muted">
                    Fleet routes · 12 DC
                  </span>
                  <div className="flex items-center gap-2 text-[10px] font-mono text-fg-muted">
                    <span className="flex items-center gap-1">
                      <span className="h-1.5 w-1.5 rounded-full bg-status-ok" />
                      {dcOk} ok
                    </span>
                    <span className="flex items-center gap-1">
                      <span className="h-1.5 w-1.5 rounded-full bg-status-warn" />
                      {dcWarn} warn
                    </span>
                    <span className="flex items-center gap-1">
                      <span className="h-1.5 w-1.5 rounded-full bg-status-error" />
                      {dcErr} err
                    </span>
                  </div>
                </div>
                <HealthRadar dcs={sortedDcs} onSelect={setSelectedDc} />
              </div>
              <div className="flex flex-col gap-3 min-w-0">
                <div className="flex items-center justify-between pb-2 border-b border-divider">
                  <span className="text-[10px] font-mono uppercase tracking-wider text-fg-muted">
                    Live telemetry{metricsChart?.timeRange ? ` · last ${metricsChart.timeRange}` : ""}
                  </span>
                  {metricsChart?.onTimeRangeChange && (
                    <div className="inline-flex items-center gap-0.5 p-0.5 rounded-xs border border-border-hi bg-bg">
                      {["5m", "1h", "6h", "24h"].map((r) => {
                        const active = metricsChart.timeRange === r;
                        return (
                          <button
                            key={r}
                            type="button"
                            onClick={() => metricsChart.onTimeRangeChange?.(r)}
                            className={cn(
                              "h-6 px-2 rounded-xs text-[10px] font-mono transition-colors",
                              active
                                ? "bg-bg-card-hi text-fg"
                                : "text-fg-muted hover:text-fg hover:bg-bg-hover",
                            )}
                          >
                            {r}
                          </button>
                        );
                      })}
                    </div>
                  )}
                </div>
                <TimelineStrip metricsPoints={timelinePoints} events={timelineEvents} />
              </div>
            </div>
          </section>

          {alertItems.length > 0 && <AlertStrip alerts={alertItems} />}

          {/* DC tiles grid — problem-first ordering already applied. */}
          <section className="flex flex-col gap-2">
            <div className="flex items-center justify-between">
              <SectionHeader title="Data Centers" badge={sortedDcs.length} />
              <span className="text-[10px] font-mono text-fg-muted">
                sorted by coverage · worst first
              </span>
            </div>
            <DcTiles dcs={sortedDcs} onSelect={setSelectedDc} />
          </section>

          {/* Gates + Upstreams in a single card, vertically split by a
              divider. Two visual languages side-by-side: dashed rows for
              boolean state flags (Gates), dark solid panels for named
              entities (Upstreams). */}
          <section className="rounded-xs bg-bg-card border border-border p-4 grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)] gap-6">
            <div className="flex flex-col gap-3">
              <span className="text-sm font-semibold text-fg">Gates</span>
              <GatesPanel gates={gates} />
            </div>
            <div className="flex flex-col gap-3 border-l border-divider pl-6">
              <div className="flex items-center justify-between">
                <span className="text-sm font-semibold text-fg">Upstreams</span>
                <span className="text-[10px] font-mono text-fg-muted">
                  {server.upstreams.length} peer{server.upstreams.length === 1 ? "" : "s"}
                </span>
              </div>
              <UpstreamsList upstreams={server.upstreams} />
            </div>
          </section>

          {/* Folds — previously tabs. Reuse the existing tab panels so
              we don't lose any data surface during the rework. */}
          {server.mePool?.enabled && (
            <Fold
              title="ME Pool"
              rightHint={`${server.mePool.summary.aliveWriters}/${server.mePool.summary.requiredWriters} writers alive`}
            >
              {mePoolContent}
            </Fold>
          )}
          {/* Top clients — keeps the per-user breakdown that used to
              sit inside Connections detail, minus the routing/lifetime
              numbers that now live in the hero pulse row. */}
          {(server.connections.topByConnections.length > 0 ||
            server.connections.topByThroughput.length > 0) && (
            <Fold
              title="Top clients"
              rightHint={`${server.connections.topByConnections.length} by conn · ${server.connections.topByThroughput.length} by traffic`}
              defaultOpen={false}
            >
              {connectionsContent}
            </Fold>
          )}
          <Fold
            title="Events"
            rightHint={`${server.events.length} entries${server.eventsDroppedTotal ? ` · ${server.eventsDroppedTotal} dropped` : ""}`}
            defaultOpen={false}
          >
            {eventsContent}
          </Fold>
        </div>

        {agentConnection && (
          <AgentConnectionSection
            data={agentConnection}
            onAllowReEnrollment={onAllowReEnrollment ?? noop}
            onRevokeGrant={onRevokeGrant ?? noop}
          />
        )}
      </div>

      {/* Shared DC detail sheet — opens from mobile strip, desktop radar, and desktop tiles. */}
      <Sheet
        open={selectedDc !== null}
        onOpenChange={(open) => {
          if (!open) setSelectedDc(null);
        }}
      >
        {/* SheetContent's backdrop onTap uses its own onOpenChange prop
            (not the Root's) — forward it here so clicking outside the
            sheet actually dismisses and doesn't leave a dead overlay
            trapping clicks. */}
        <SheetContent
          side="bottom"
          onOpenChange={(open) => {
            if (!open) setSelectedDc(null);
          }}
        >
          {selectedDc && (
            <>
              <SheetHeader>
                <SheetTitle>DC{selectedDc.dc} Details</SheetTitle>
              </SheetHeader>
              <SheetBody>
                <div className="flex flex-col gap-4">
                  <div className="grid grid-cols-2 gap-x-4 gap-y-1.5 text-sm">
                    <span className="text-fg-muted">Coverage</span>
                    <span
                      className={`font-mono font-semibold ${coverageColor(selectedDc.coveragePct)}`}
                    >
                      {selectedDc.coveragePct}%
                    </span>
                    <span className="text-fg-muted">Available</span>
                    <span
                      className={`font-mono ${selectedDc.availablePct < 100 ? "text-status-warn" : "text-fg"}`}
                    >
                      {selectedDc.availablePct}%
                    </span>
                    <span className="text-fg-muted">Writers</span>
                    <span className="font-mono text-fg">
                      {selectedDc.aliveWriters}/{selectedDc.requiredWriters} alive
                    </span>
                    <span className="text-fg-muted">RTT</span>
                    <span
                      className={`font-mono ${(selectedDc.rttMs ?? 0) > 300 ? "text-status-error" : (selectedDc.rttMs ?? 0) > 100 ? "text-status-warn" : "text-fg"}`}
                    >
                      {selectedDc.rttMs != null ? `${selectedDc.rttMs}ms` : "—"}
                    </span>
                    <span className="text-fg-muted">Load</span>
                    <span className="font-mono text-fg">{selectedDc.load}</span>
                    <span className="text-fg-muted">Floor</span>
                    <span className="font-mono text-fg">
                      {selectedDc.floorMin}..{selectedDc.floorTarget}..{selectedDc.floorMax}
                      {selectedDc.floorCapped && (
                        <span className="text-status-warn ml-1">⚠ capped</span>
                      )}
                    </span>
                  </div>

                  {selectedDc.endpointWriters.length > 0 && (
                    <div className="flex flex-col gap-2">
                      <FieldLabel>Endpoints & Writers</FieldLabel>
                      {selectedDc.endpointWriters.map((ew) => (
                        <div key={ew.endpoint} className="flex items-center gap-2 text-sm">
                          <MonoValue>{ew.endpoint}</MonoValue>
                          <span className="text-fg-muted">→</span>
                          <MonoValue>
                            {ew.activeWriters} active writer{ew.activeWriters !== 1 ? "s" : ""}
                          </MonoValue>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              </SheetBody>
            </>
          )}
        </SheetContent>
      </Sheet>

      <Sheet open={renameOpen} onOpenChange={setRenameOpen}>
        <SheetContent>
          <SheetHeader>
            <SheetTitle>Rename Server</SheetTitle>
          </SheetHeader>
          <SheetBody>
            <form
              onSubmit={(e) => {
                e.preventDefault();
                const trimmed = renameValue.trim();
                if (trimmed && trimmed !== server.name) {
                  onRename?.(trimmed);
                }
                setRenameOpen(false);
              }}
              className="flex flex-col gap-4"
            >
              <label className="flex flex-col gap-1.5">
                <span className="text-sm text-fg-muted">Server Name</span>
                <input
                  type="text"
                  value={renameValue}
                  onChange={(e) => setRenameValue(e.target.value)}
                  className="rounded-xs border border-border bg-bg px-3 py-2 text-sm text-fg focus:outline-none focus:ring-2 focus:ring-accent"
                  autoFocus
                />
              </label>
              <div className="flex justify-end gap-2">
                <button
                  type="button"
                  onClick={() => setRenameOpen(false)}
                  className="px-3 py-1.5 text-sm rounded-xs border border-border text-fg hover:bg-bg-card-hover transition-colors"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  disabled={!renameValue.trim() || renameValue.trim() === server.name}
                  className="px-3 py-1.5 text-sm rounded-xs bg-accent text-white hover:bg-accent/90 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                >
                  Save
                </button>
              </div>
            </form>
          </SheetBody>
        </SheetContent>
      </Sheet>

      {deregisterOpen && (
        <div className="fixed inset-0 z-50 flex items-center justify-center">
          <div className="absolute inset-0 bg-black/60" onClick={() => setDeregisterOpen(false)} />
          <div className="relative z-10 bg-bg-card border border-border rounded-lg shadow-xl p-6 max-w-sm w-full mx-4">
            <h3 className="text-base font-semibold text-fg mb-2">Deregister Server</h3>
            <p className="text-sm text-fg-muted mb-4">
              This will disconnect the agent, revoke its credentials, and remove all associated
              data. This action cannot be undone.
            </p>
            <div className="flex justify-end gap-2">
              <button
                onClick={() => setDeregisterOpen(false)}
                className="px-3 py-1.5 text-sm rounded-xs border border-border text-fg hover:bg-bg-card-hover transition-colors"
              >
                Cancel
              </button>
              <button
                onClick={() => {
                  onDeregister?.();
                  setDeregisterOpen(false);
                }}
                className="px-3 py-1.5 text-sm rounded-xs bg-status-error text-white hover:bg-status-error/90 transition-colors"
              >
                Deregister
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
