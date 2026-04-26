import { useState } from "react";
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

function formatTime(value: string) {
  const d = new Date(value);
  return d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

function formatDate(value: string) {
  const d = new Date(value);
  return d.toLocaleDateString([], { month: "short", day: "numeric" });
}

function ChartContainer({ children }: { children: React.ReactNode }) {
  return (
    <div className={chartContainerClass}>
      <ResponsiveContainer width="100%" height="100%">
        {children as React.ReactElement}
      </ResponsiveContainer>
    </div>
  );
}

function SystemChart({ points }: { points: MetricsPoint[] }) {
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
          tick={{ fontSize: 11 }}
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
          stroke="#3b82f6"
          fill="#3b82f6"
          fillOpacity={0.15}
          strokeWidth={1.5}
          dot={false}
        />
        <Area
          type="monotone"
          dataKey="memAvg"
          name="Memory"
          stroke="#8b5cf6"
          fill="#8b5cf6"
          fillOpacity={0.15}
          strokeWidth={1.5}
          dot={false}
        />
        <Area
          type="monotone"
          dataKey="diskAvg"
          name="Disk"
          stroke="#f59e0b"
          fill="#f59e0b"
          fillOpacity={0.1}
          strokeWidth={1.5}
          dot={false}
        />
      </AreaChart>
    </ChartContainer>
  );
}

function ConnectionsChart({ points }: { points: MetricsPoint[] }) {
  return (
    <ChartContainer>
      <AreaChart data={points} margin={{ top: 8, right: 8, left: 0, bottom: 0 }}>
        <CartesianGrid strokeDasharray="3 3" stroke="var(--color-border)" />
        <XAxis
          dataKey="t"
          tickFormatter={formatTime}
          tick={{ fontSize: 11, fill: "var(--color-fg-muted)" }}
        />
        <YAxis tick={{ fontSize: 11 }} className="text-fg-muted" />
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
          stroke="#3b82f6"
          fill="#3b82f6"
          fillOpacity={0.2}
          strokeWidth={1.5}
          dot={false}
        />
        <Area
          type="monotone"
          dataKey="activeUsersAvg"
          name="Active Users"
          stroke="#10b981"
          fill="#10b981"
          fillOpacity={0.15}
          strokeWidth={1.5}
          dot={false}
        />
      </AreaChart>
    </ChartContainer>
  );
}

function NetworkChart({ points }: { points: MetricsPoint[] }) {
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
          tick={{ fontSize: 11 }}
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
          stroke="#ef4444"
          fill="#ef4444"
          fillOpacity={0.1}
          strokeWidth={1.5}
          dot={false}
        />
      </AreaChart>
    </ChartContainer>
  );
}

function TrafficChart({ points }: { points: MetricsPoint[] }) {
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
          tick={{ fontSize: 11 }}
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
          stroke="#3b82f6"
          fill="#3b82f6"
          fillOpacity={0.15}
          strokeWidth={1.5}
          dot={false}
        />
        <Area
          type="monotone"
          dataKey="netDownloadMbps"
          name="Download"
          stroke="#10b981"
          fill="#10b981"
          fillOpacity={0.15}
          strokeWidth={1.5}
          dot={false}
        />
      </AreaChart>
    </ChartContainer>
  );
}

export default function MetricsChartSectionInner({
  points,
  resolution,
  timeRange,
  onTimeRangeChange,
  availableRanges = TIME_RANGES,
}: MetricsChartSectionProps) {
  const [tab, setTab] = useState<MetricsTab>("system");

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center justify-between">
        <SectionHeader title="Performance" />
        <div className="flex items-center gap-2">
          {resolution && (
            <span className="text-[10px] text-fg-muted bg-bg px-1.5 py-0.5 rounded-xs border border-border">
              {resolution}
            </span>
          )}
          <div className="flex rounded-xs border border-border overflow-hidden">
            {availableRanges.map((r) => (
              <button
                key={r}
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

      <div className="flex gap-1 border-b border-border">
        {(["system", "connections", "network", "traffic"] as MetricsTab[]).map((t) => (
          <button
            key={t}
            onClick={() => setTab(t)}
            className={`px-3 py-1.5 text-xs capitalize transition-colors border-b-2 -mb-px ${
              tab === t ? "border-accent text-fg" : "border-transparent text-fg-muted hover:text-fg"
            }`}
          >
            {t}
          </button>
        ))}
      </div>

      <div className="bg-bg-card border border-border rounded-lg p-4">
        {points.length === 0 ? (
          <div className={chartFallbackClass}>
            No data available for this time range
          </div>
        ) : (
          <>
            {tab === "system" && <SystemChart points={points} />}
            {tab === "connections" && <ConnectionsChart points={points} />}
            {tab === "network" && <NetworkChart points={points} />}
            {tab === "traffic" && <TrafficChart points={points} />}
          </>
        )}
      </div>
    </div>
  );
}
