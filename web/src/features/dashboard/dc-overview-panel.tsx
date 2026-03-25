import { Globe } from "lucide-react";
import type { ReactNode } from "react";
import { SectionPanel } from "@/components/section-panel";
import type { Agent } from "@/lib/api";
import {
  buildFleetDcCoverageSummary,
  type FleetDcCoverageRow,
} from "./dashboard-view-model";

interface DcOverviewPanelProps {
  agents: Agent[];
}

function getRttColor(rtt: number): string {
  if (rtt < 100) return "text-good-text";
  if (rtt <= 500) return "text-warn-text";
  return "text-bad-text";
}

export function DcOverviewPanel({ agents }: DcOverviewPanelProps) {
  const summary = buildFleetDcCoverageSummary(agents);
  const rows = summary.rows
    .filter((row) => row.health !== "ok")
    .sort((left, right) => {
      const healthDelta = getHealthPriority(right) - getHealthPriority(left);
      if (healthDelta !== 0) {
        return healthDelta;
      }

      return left.dc - right.dc;
    });
  const icon: ReactNode = <Globe className="w-4 h-4" />;
  const affectedLabel = rows.length === 1 ? "1 affected" : `${rows.length} affected`;

  return (
    <SectionPanel
      headerRight={
        <span className="text-[11px] font-semibold text-text-3">
          {affectedLabel}
        </span>
      }
      icon={icon}
      title="DC Degradation"
    >
      {rows.length === 0 ? (
        <div className="flex items-center justify-center p-8">
          <span className="text-text-3 text-sm">All tracked DCs are healthy.</span>
        </div>
      ) : (
        <table className="w-full">
          <thead>
            <tr className="border-b border-border">
              <th className="text-[10px] font-bold text-text-3 uppercase tracking-[0.1em] px-4 py-2 text-left">DC</th>
              <th className="text-[10px] font-bold text-text-3 uppercase tracking-[0.1em] px-4 py-2 text-right">Servers</th>
              <th className="text-[10px] font-bold text-text-3 uppercase tracking-[0.1em] px-4 py-2 text-right">Avg RTT</th>
              <th className="text-[10px] font-bold text-text-3 uppercase tracking-[0.1em] px-4 py-2 text-right">Health</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => (
              <tr key={row.dc} className="border-b border-border last:border-0">
                <td className="px-4 py-2 text-sm text-text-1 font-medium">{row.dc}</td>
                <td className="px-4 py-2 text-sm text-text-2 text-right font-mono">
                  {row.serverCount} affected
                </td>
                <td className={`px-4 py-2 text-sm text-right font-mono ${getRttColor(row.averageRttMs)}`}>
                  {row.averageRttMs > 0 ? `${row.averageRttMs}ms` : "—"}
                </td>
                <td className="px-4 py-2 text-right">
                  <span className={getHealthClassName(row.health)}>
                    {getHealthLabel(row.health)}
                  </span>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </SectionPanel>
  );
}

function getHealthPriority(row: FleetDcCoverageRow): number {
  if (row.health === "down") {
    return 2;
  }

  if (row.health === "partial") {
    return 1;
  }

  return 0;
}

function getHealthLabel(health: FleetDcCoverageRow["health"]): string {
  if (health === "down") {
    return "Down";
  }

  return "Partial";
}

function getHealthClassName(health: FleetDcCoverageRow["health"]): string {
  if (health === "down") {
    return "text-bad-text text-sm font-semibold";
  }

  return "text-warn-text text-sm font-semibold";
}
