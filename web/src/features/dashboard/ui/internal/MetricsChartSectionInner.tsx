import { useId, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  AreaChart,
  Area,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
  Legend,
} from "recharts";
import { SectionHeader } from "@/ui/layout/SectionHeader";
import {
  AccessibleChart,
  type AccessibleChartSeries,
} from "@/ui/components/AccessibleChart";
import { chartContainerClass, chartFallbackClass } from "./chartContainer";

export interface MetricsPoint {
  t: string;
  cpuAvg?: number;
  cpuMax?: number;
  memAvg?: number;
  memMax?: number;
  diskAvg?: number;
  diskMax?: number;
  connectionsAvg?: number;
  connectionsMax?: number;
  activeUsersAvg?: number;
  activeUsersMax?: number;
  dcCoverageMin?: number;
  load1m?: number;
  netUploadMbps?: number;
  netDownloadMbps?: number;
}

export type MetricsTab = "system" | "connections" | "network" | "traffic";

export interface MetricsChartSectionProps {
  points: MetricsPoint[];
  resolution?: "raw" | "hourly" | undefined;
  timeRange: string;
  onTimeRangeChange?: ((range: string) => void) | undefined;
  availableRanges?: string[] | undefined;
}

const TIME_RANGES = ["1h", "6h", "24h", "7d"];

const TAB_KEYS: MetricsTab[] = ["system", "connections", "network", "traffic"];

function formatTime(value: string) {
  const d = new Date(value);
  return d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

function formatDate(value: string) {
  const d = new Date(value);
  return d.toLocaleDateString([], { month: "short", day: "numeric" });
}

function ChartContainer({ children }: Readonly<{ children: React.ReactNode }>) {
  return (
    <div className={chartContainerClass}>
      <ResponsiveContainer width="100%" height="100%">
        {children as React.ReactElement}
      </ResponsiveContainer>
    </div>
  );
}

function SystemChart({ points }: Readonly<{ points: MetricsPoint[] }>) {
  return (
    <ChartContainer>
      <AreaChart data={points} margin={{ top: 8, right: 8, left: 0, bottom: 0 }}>
        <CartesianGrid strokeDasharray="3 3" stroke="var(--color-border)" />
        <XAxis
          dataKey="t"
          tickFormatter={formatTime}
          tick={{ fontSize: 11, fill: "var(--color-fg-muted)" }}
        />
        <YAxis
          domain={[0, 100]}
          tick={{ fontSize: 11, fill: "var(--color-fg-muted)" }}
          tickFormatter={(v) => `${v}%`}
          className="text-fg-muted"
        />
        <Tooltip
          contentStyle={{
            backgroundColor: "var(--color-bg-card)",
            border: "1px solid var(--color-border)",
            borderRadius: 6,
            color: "var(--color-fg-muted)",
          }}
          labelFormatter={(label) => formatDate(String(label))}
          formatter={(value, name) => [`${Number(value).toFixed(1)}%`, name]}
        />
        <Legend />
        <Area
          type="monotone"
          dataKey="cpuAvg"
          name="CPU"
          stroke="var(--color-chart-1)"
          fill="var(--color-chart-1)"
          fillOpacity={0.15}
          strokeWidth={1.5}
          dot={false}
        />
        <Area
          type="monotone"
          dataKey="memAvg"
          name="Memory"
          stroke="var(--color-chart-2)"
          fill="var(--color-chart-2)"
          fillOpacity={0.15}
          strokeWidth={1.5}
          dot={false}
        />
        <Area
          type="monotone"
          dataKey="diskAvg"
          name="Disk"
          stroke="var(--color-status-warn)"
          fill="var(--color-status-warn)"
          fillOpacity={0.1}
          strokeWidth={1.5}
          dot={false}
        />
      </AreaChart>
    </ChartContainer>
  );
}

function ConnectionsChart({ points }: Readonly<{ points: MetricsPoint[] }>) {
  return (
    <ChartContainer>
      <AreaChart data={points} margin={{ top: 8, right: 8, left: 0, bottom: 0 }}>
        <CartesianGrid strokeDasharray="3 3" stroke="var(--color-border)" />
        <XAxis
          dataKey="t"
          tickFormatter={formatTime}
          tick={{ fontSize: 11, fill: "var(--color-fg-muted)" }}
        />
        <YAxis tick={{ fontSize: 11, fill: "var(--color-fg-muted)" }} className="text-fg-muted" />
        <Tooltip
          contentStyle={{
            backgroundColor: "var(--color-bg-card)",
            border: "1px solid var(--color-border)",
            borderRadius: 6,
            color: "var(--color-fg-muted)",
          }}
          labelFormatter={(label) => formatDate(String(label))}
        />
        <Legend />
        <Area
          type="monotone"
          dataKey="connectionsAvg"
          name="Connections"
          stroke="var(--color-chart-1)"
          fill="var(--color-chart-1)"
          fillOpacity={0.2}
          strokeWidth={1.5}
          dot={false}
        />
        <Area
          type="monotone"
          dataKey="activeUsersAvg"
          name="Active Users"
          stroke="var(--color-status-ok)"
          fill="var(--color-status-ok)"
          fillOpacity={0.15}
          strokeWidth={1.5}
          dot={false}
        />
      </AreaChart>
    </ChartContainer>
  );
}

function NetworkChart({ points }: Readonly<{ points: MetricsPoint[] }>) {
  return (
    <ChartContainer>
      <AreaChart data={points} margin={{ top: 8, right: 8, left: 0, bottom: 0 }}>
        <CartesianGrid strokeDasharray="3 3" stroke="var(--color-border)" />
        <XAxis
          dataKey="t"
          tickFormatter={formatTime}
          tick={{ fontSize: 11, fill: "var(--color-fg-muted)" }}
        />
        <YAxis
          domain={[0, 100]}
          tick={{ fontSize: 11, fill: "var(--color-fg-muted)" }}
          tickFormatter={(v) => `${v}%`}
          className="text-fg-muted"
        />
        <Tooltip
          contentStyle={{
            backgroundColor: "var(--color-bg-card)",
            border: "1px solid var(--color-border)",
            borderRadius: 6,
            color: "var(--color-fg-muted)",
          }}
          labelFormatter={(label) => formatDate(String(label))}
          formatter={(value, name) => [`${Number(value).toFixed(1)}%`, name]}
        />
        <Legend />
        <Area
          type="monotone"
          dataKey="dcCoverageMin"
          name="DC Coverage (min)"
          stroke="var(--color-status-error)"
          fill="var(--color-status-error)"
          fillOpacity={0.1}
          strokeWidth={1.5}
          dot={false}
        />
      </AreaChart>
    </ChartContainer>
  );
}

function TrafficChart({ points }: Readonly<{ points: MetricsPoint[] }>) {
  return (
    <ChartContainer>
      <AreaChart data={points} margin={{ top: 8, right: 8, left: 0, bottom: 0 }}>
        <CartesianGrid strokeDasharray="3 3" stroke="var(--color-border)" />
        <XAxis
          dataKey="t"
          tickFormatter={formatTime}
          tick={{ fontSize: 11, fill: "var(--color-fg-muted)" }}
        />
        <YAxis
          tick={{ fontSize: 11, fill: "var(--color-fg-muted)" }}
          tickFormatter={(v) => `${v.toFixed(1)}`}
          className="text-fg-muted"
        />
        <Tooltip
          contentStyle={{
            backgroundColor: "var(--color-bg-card)",
            border: "1px solid var(--color-border)",
            borderRadius: 6,
            color: "var(--color-fg-muted)",
          }}
          labelFormatter={(label) => formatDate(String(label))}
          formatter={(value, name) => [`${Number(value).toFixed(2)} Mbps`, name]}
        />
        <Legend />
        <Area
          type="monotone"
          dataKey="netUploadMbps"
          name="Upload"
          stroke="var(--color-chart-1)"
          fill="var(--color-chart-1)"
          fillOpacity={0.15}
          strokeWidth={1.5}
          dot={false}
        />
        <Area
          type="monotone"
          dataKey="netDownloadMbps"
          name="Download"
          stroke="var(--color-status-ok)"
          fill="var(--color-status-ok)"
          fillOpacity={0.15}
          strokeWidth={1.5}
          dot={false}
        />
      </AreaChart>
    </ChartContainer>
  );
}

// Series metadata for the screen-reader data table (mirrors each chart's
// rendered <Area>s). Kept beside the chart components so the two stay
// in sync.
const TAB_SERIES: Record<MetricsTab, AccessibleChartSeries[]> = {
  system: [
    { key: "cpuAvg", label: "CPU" },
    { key: "memAvg", label: "Memory" },
    { key: "diskAvg", label: "Disk" },
  ],
  connections: [
    { key: "connectionsAvg", label: "Connections" },
    { key: "activeUsersAvg", label: "Active Users" },
  ],
  network: [{ key: "dcCoverageMin", label: "DC Coverage (min)" }],
  traffic: [
    { key: "netUploadMbps", label: "Upload" },
    { key: "netDownloadMbps", label: "Download" },
  ],
};

const TAB_UNIT: Record<MetricsTab, string | undefined> = {
  system: "%",
  connections: undefined,
  network: "%",
  traffic: " Mbps",
};

export default function MetricsChartSectionInner({
  points,
  resolution,
  timeRange,
  onTimeRangeChange,
  availableRanges = TIME_RANGES,
}: Readonly<MetricsChartSectionProps>) {
  const { t } = useTranslation("dashboard");
  const [tab, setTab] = useState<MetricsTab>("system");
  const tabRefs = useRef<Record<MetricsTab, HTMLButtonElement | null>>({
    system: null,
    connections: null,
    network: null,
    traffic: null,
  });
  const idBase = useId();
  const tabId = (key: MetricsTab) => `${idBase}-tab-${key}`;
  const panelId = `${idBase}-panel`;

  // U1: roving-tabindex keyboard nav — arrows move focus + activate,
  // Home/End jump to the ends. Matches the WAI-ARIA "tabs with automatic
  // activation" pattern.
  const handleTabKeyDown = (e: React.KeyboardEvent, index: number) => {
    if (
      e.key !== "ArrowLeft" &&
      e.key !== "ArrowRight" &&
      e.key !== "Home" &&
      e.key !== "End"
    ) {
      return;
    }
    e.preventDefault();
    let nextIndex = index;
    if (e.key === "ArrowLeft") nextIndex = (index - 1 + TAB_KEYS.length) % TAB_KEYS.length;
    if (e.key === "ArrowRight") nextIndex = (index + 1) % TAB_KEYS.length;
    if (e.key === "Home") nextIndex = 0;
    if (e.key === "End") nextIndex = TAB_KEYS.length - 1;
    const nextKey = TAB_KEYS[nextIndex];
    if (!nextKey) return;
    setTab(nextKey);
    tabRefs.current[nextKey]?.focus();
  };

  const renderActiveChart = () => {
    switch (tab) {
      case "system":
        return <SystemChart points={points} />;
      case "connections":
        return <ConnectionsChart points={points} />;
      case "network":
        return <NetworkChart points={points} />;
      case "traffic":
        return <TrafficChart points={points} />;
    }
  };

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center justify-between">
        <SectionHeader title="Performance" />
        <div className="flex items-center gap-2">
          {resolution && (
            <span className="text-nano text-fg-muted bg-bg px-1.5 py-0.5 rounded-xs border border-border">
              {resolution}
            </span>
          )}
          <div
            role="group"
            aria-label={t("metrics.rangeLabel")}
            className="flex rounded-xs border border-border overflow-hidden"
          >
            {availableRanges.map((r) => (
              <button
                key={r}
                type="button"
                aria-pressed={timeRange === r}
                onClick={() => onTimeRangeChange?.(r)}
                className={`px-2 py-1 text-xs transition-colors ${
                  timeRange === r ? "bg-accent text-white" : "text-fg-muted hover:bg-bg-card-hover"
                }`}
              >
                {r}
              </button>
            ))}
          </div>
        </div>
      </div>

      <div
        role="tablist"
        aria-label={t("metrics.tablistLabel")}
        className="flex gap-1 border-b border-border"
      >
        {TAB_KEYS.map((key, index) => {
          const selected = tab === key;
          return (
            <button
              key={key}
              ref={(el) => {
                tabRefs.current[key] = el;
              }}
              type="button"
              role="tab"
              id={tabId(key)}
              aria-selected={selected}
              aria-controls={panelId}
              tabIndex={selected ? 0 : -1}
              onClick={() => setTab(key)}
              onKeyDown={(e) => handleTabKeyDown(e, index)}
              className={`px-3 py-1.5 text-xs capitalize transition-colors border-b-2 -mb-px ${
                selected ? "border-accent text-fg" : "border-transparent text-fg-muted hover:text-fg"
              }`}
            >
              {t(`metrics.tabs.${key}`)}
            </button>
          );
        })}
      </div>

      <div
        role="tabpanel"
        id={panelId}
        aria-labelledby={tabId(tab)}
        tabIndex={0}
        className="bg-bg-card border border-border rounded-lg p-4"
      >
        {points.length === 0 ? (
          <div className={chartFallbackClass}>{t("metrics.noData")}</div>
        ) : (
          <AccessibleChart<MetricsPoint>
            title={t(`metrics.tabs.${tab}`)}
            labelKey="t"
            labelHeader={t("metrics.timeColumn")}
            formatLabel={(value) => formatTime(String(value))}
            unit={TAB_UNIT[tab]}
            series={TAB_SERIES[tab]}
            data={points}
          >
            {renderActiveChart()}
          </AccessibleChart>
        )}
      </div>
    </div>
  );
}
