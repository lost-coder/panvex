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
      {/* Mobile: 2×2 tiles — large values, legible labels (was a 12px
          inline string that forced squinting). */}
      <div className="grid grid-cols-2 gap-2 md:hidden">
        {kpis.map((k) => {
          const tone: NonNullable<KpiItem["tone"]> = k.tone ?? "default";
          return (
            <div
              key={k.label}
              className="rounded-sm bg-bg-card border border-border px-3.5 py-3 flex flex-col gap-0.5"
            >
              <span className="text-xs uppercase tracking-wider text-fg-muted">{k.label}</span>
              <span
                className={`text-2xl font-mono font-bold leading-none tracking-tight ${TONE_VALUE_CLASS[tone]}`}
              >
                {k.value}
              </span>
            </div>
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
                  <span className="text-nano text-fg-muted uppercase tracking-wider">
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
              <div className="flex items-center gap-2 text-nano font-mono text-fg-muted mt-auto">
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
