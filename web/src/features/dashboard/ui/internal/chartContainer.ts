// Q5.U-Q-12: dashboard chart sizing constants. Centralised so a future
// "operators want bigger sparklines" change is one edit, not a grep
// across MetricsChartSection*. The rendered heights remain Tailwind
// arbitrary values — Recharts measures the parent's pixel height
// directly, so a rem-based class would still need an exact pixel
// breakpoint here.
export const chartContainerClass = "h-[280px] w-full";
export const chartFallbackClass = "h-[280px] flex items-center justify-center text-sm text-fg-muted";
export const chartShortFallbackClass = "flex items-center justify-center h-[260px] text-fg-muted text-xs";
