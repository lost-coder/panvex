import {
  AlertStrip,
  InitCard,
  SectionHeader,
  SwipeTabView,
} from "@/ui";
import { DCScrollStrip } from "@/features/servers/ui/DCScrollStrip";
import { MetricsChartSection } from "@/features/dashboard/ui/MetricsChartSection";
import type { ServerDcData, ServerDetailPageProps } from "@/shared/api/types-pages/pages";

import { PulseGrid, type PulseTickData } from "./PulseGrid";
import type { AlertItem, DCStripItem } from "../format";

/**
 * Mobile column for the server detail page. Renders init card, KPI
 * pulse grid, alerts, telemetry chart, the DC scroll strip, and the
 * legacy swipe tabs. The desktop story lives in `DesktopLayout`.
 */
export function MobileLayout({
  initState,
  pulseItems,
  alertItems,
  metricsChart,
  sortedDcs,
  dcItems,
  mobileTabs,
  onSelectDc,
}: Readonly<{
  initState: ServerDetailPageProps["initState"];
  pulseItems: PulseTickData[];
  alertItems: AlertItem[];
  metricsChart: ServerDetailPageProps["metricsChart"];
  sortedDcs: ServerDcData[];
  dcItems: DCStripItem[];
  mobileTabs: { id: string; label: string; content: React.ReactNode }[];
  onSelectDc: (dc: Readonly<ServerDcData>) => void;
}>) {
  return (
    <div className="md:hidden flex flex-col gap-4">
      {/* Gates moved into the "Gates & Upstreams" swipe tab; the
          badge row would have duplicated that signal. */}
      {initState && <InitCard {...initState} />}
      {/* Pulse tickers in a 2×2 grid with vertical + horizontal
          dividers between every cell. */}
      <PulseGrid variant="mobile" items={pulseItems} />
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
            const match = sortedDcs.find((d) => d.dc === dcNum);
            if (match) onSelectDc(match);
          }}
        />
      </div>
      <SwipeTabView tabs={mobileTabs} />
    </div>
  );
}
