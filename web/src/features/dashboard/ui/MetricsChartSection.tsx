import { lazy, Suspense } from "react";
import type {
  MetricsChartSectionProps,
  MetricsPoint,
  MetricsTab,
} from "./internal/MetricsChartSectionInner";
import { chartShortFallbackClass } from "./internal/chartContainer";

export type { MetricsChartSectionProps, MetricsPoint, MetricsTab };

// U8: recharts pulls roughly 90 kB (gzipped) of chart + d3 internals.
// Routes that never show metrics charts (Login, Clients, Users, Settings)
// should not pay for it. The inner component is the only place that
// imports recharts, so splitting it off lets bundlers route every
// recharts symbol into its own async chunk. A small Suspense fallback
// keeps layout stable while the chunk streams on first visit.
const MetricsChartSectionInner = lazy(() =>
  import("./internal/MetricsChartSectionInner"),
);

export function MetricsChartSection(props: Readonly<MetricsChartSectionProps>) {
  return (
    <Suspense
      fallback={
        <div
          className={chartShortFallbackClass}
          role="status"
          aria-label="Loading charts"
        >
          …
        </div>
      }
    >
      <MetricsChartSectionInner {...props} />
    </Suspense>
  );
}
