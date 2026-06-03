import {
  MiniChart,
  deltaClass,
  deltaArrow,
  type DashboardOverviewData,
  type KpiItem,
} from "@/ui";

const TONE_VALUE_CLASS: Record<NonNullable<KpiItem["tone"]>, string> = {
  default: "text-fg",
  ok: "text-status-ok",
  warn: "text-status-warn",
  error: "text-status-error",
};

const SPARKLINE_COLOR_BY_TONE: Record<NonNullable<KpiItem["tone"]>, string> = {
  default: "var(--color-accent)",
  ok: "var(--color-status-ok)",
  warn: "var(--color-status-warn)",
  error: "var(--color-status-error)",
};

export function KpiStrip({ kpis }: Readonly<{ kpis: DashboardOverviewData["kpis"] }>) {
  return (
    <>
      {/* Mobile: compact text — keeps the 4 KPIs in one line of signal */}
      <div className="flex flex-wrap items-center gap-x-5 gap-y-1 text-xs font-mono md:hidden">
        {kpis.map((k) => {
          const valueClass = k.tone ? TONE_VALUE_CLASS[k.tone] : "text-fg";
          return (
            <span key={k.label} className="text-fg-muted">
              {k.label.toLowerCase()}{" "}
              <span className={`font-medium ${valueClass}`}>{k.value}</span>
            </span>
          );
        })}
      </div>
      {/* Desktop: dense tiles — value + sparkline (if provided), delta + sub underneath */}
      <div className="hidden md:grid grid-cols-4 gap-3">
        {kpis.map((k) => {
          const tone: NonNullable<KpiItem["tone"]> = k.tone ?? "default";
          return (
            <div
              key={k.label}
              className="rounded-xs bg-bg-card border border-border px-4 py-3 flex flex-col gap-1 min-h-[88px]"
            >
              <div className="flex items-start justify-between gap-3">
                <div className="flex flex-col gap-0.5 min-w-0">
                  <span className="text-[10px] text-fg-muted uppercase tracking-wider">
                    {k.label}
                  </span>
                  <span
                    className={`text-2xl font-mono font-semibold leading-none tracking-tight ${TONE_VALUE_CLASS[tone]}`}
                  >
                    {k.value}
                  </span>
                </div>
                {k.series && k.series.length > 1 && (
                  <MiniChart
                    data={k.series}
                    width={90}
                    height={36}
                    color={SPARKLINE_COLOR_BY_TONE[tone]}
                  />
                )}
              </div>
              <div className="flex items-center gap-2 text-[10px] font-mono text-fg-muted mt-auto">
                {k.deltaLabel && (
                  <span className={deltaClass(k.deltaDirection)}>
                    {deltaArrow(k.deltaDirection)}{" "}
                    {k.deltaLabel}
                  </span>
                )}
                {k.sub && <span>{k.sub}</span>}
              </div>
            </div>
          );
        })}
      </div>
    </>
  );
}
