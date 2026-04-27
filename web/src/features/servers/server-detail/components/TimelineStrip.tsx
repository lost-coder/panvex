import { memo } from "react";

import type { MetricsPoint } from "@/features/dashboard/ui/internal/MetricsChartSectionInner";

// ─── Timeline strip (two sparklines + event pins) ─────────────────────
// Plots connections + bad-rate over the available metric window with
// dashed vertical pins where recent events land. Falls back to the
// MetricsChartSection when the backend hasn't supplied series yet.
//
// Memoised — `metricsPoints` and `events` come from memoised parent
// values, so the SVG normalisation only re-runs when the upstream
// telemetry actually changes.
export const TimelineStrip = memo(_TimelineStrip);

function _TimelineStrip({
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
  const tMax = tsOf(metricsPoints.at(-1)!);
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
        {events.map((e) => {
          const pct = (e.tsEpochSecs - tMin) / range;
          if (pct < 0 || pct > 1) return null;
          const x = pad + pct * innerW;
          const color = (() => {
            if (e.kind === "error") return "var(--color-status-error)";
            if (e.kind === "warn") return "var(--color-status-warn)";
            return "var(--color-fg-muted)";
          })();
          // Composite key: timestamp + kind disambiguates pins that
          // share a millisecond but represent different signals.
          return (
            <g key={`${e.tsEpochSecs}-${e.kind}`}>
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
