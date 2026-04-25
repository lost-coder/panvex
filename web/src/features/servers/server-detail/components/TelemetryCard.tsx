import { cn } from "@/ui";
import type { ServerDcData, ServerDetailPageProps } from "@/shared/api/types-pages/pages";

import { HealthRadar } from "./HealthRadar";
import { TimelineStrip } from "./TimelineStrip";
import type { TimelineEvent } from "../format";

/**
 * Health radar + live telemetry share one card with NO vertical
 * divider between them — instead each column has a label row underlined
 * by a horizontal border, mirroring the handoff design where the two
 * contents read as one continuous panel split only by their headings.
 */
export function TelemetryCard({
  sortedDcs,
  dcOk,
  dcWarn,
  dcErr,
  metricsChart,
  timelineEvents,
  onSelectDc,
}: {
  sortedDcs: ServerDcData[];
  dcOk: number;
  dcWarn: number;
  dcErr: number;
  metricsChart: ServerDetailPageProps["metricsChart"];
  timelineEvents: TimelineEvent[];
  onSelectDc: (dc: ServerDcData) => void;
}) {
  const timelinePoints = metricsChart?.points ?? [];
  return (
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
          <HealthRadar dcs={sortedDcs} onSelect={onSelectDc} />
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
  );
}
