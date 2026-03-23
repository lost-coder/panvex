import { Globe } from "lucide-react";
import type { ReactNode } from "react";
import { SectionPanel } from "@/components/section-panel";
import { DcHealthBar } from "@/components/ui/dc-health-bar";

interface DcOverviewPanelProps {
  agents: any[];
}

function getRttColor(rtt: number): string {
  if (rtt < 100) return "text-good-text";
  if (rtt <= 500) return "text-warn-text";
  return "text-bad-text";
}

export function DcOverviewPanel({ agents }: DcOverviewPanelProps) {
  // Aggregate agents by DC name
  const dcMap = new Map<string, { servers: any[]; totalRtt: number; rttCount: number }>();

  for (const agent of agents) {
    const dcName = agent.dc_name ?? agent.datacenter ?? agent.dc ?? "Unknown";
    if (!dcMap.has(dcName)) {
      dcMap.set(dcName, { servers: [], totalRtt: 0, rttCount: 0 });
    }
    const entry = dcMap.get(dcName)!;
    entry.servers.push(agent);
    const rtt = agent.rtt_ms ?? agent.latency_ms ?? agent.avg_rtt ?? 0;
    if (rtt > 0) {
      entry.totalRtt += rtt;
      entry.rttCount += 1;
    }
  }

  const rows = Array.from(dcMap.entries()).map(([dcName, data]) => ({
    dcName,
    serverCount: data.servers.length,
    avgRtt: data.rttCount > 0 ? Math.round(data.totalRtt / data.rttCount) : 0,
    agents: data.servers,
  }));

  return (
    <SectionPanel icon={<Globe className="w-4 h-4" />} title="DC Overview">
      {rows.length === 0 ? (
        <div className="flex items-center justify-center p-8">
          <span className="text-text-3 text-sm">No datacenters found</span>
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
              <tr key={row.dcName} className="border-b border-border last:border-0">
                <td className="px-4 py-2 text-sm text-text-1 font-medium">{row.dcName}</td>
                <td className="px-4 py-2 text-sm text-text-2 text-right font-mono">{row.serverCount}</td>
                <td className={`px-4 py-2 text-sm text-right font-mono ${getRttColor(row.avgRtt)}`}>
                  {row.avgRtt > 0 ? `${row.avgRtt}ms` : "—"}
                </td>
                <td className="px-4 py-2 text-right">
                  <div className="flex justify-end">
                    <DcHealthBar segments={row.agents.map((a: any) => a.presence_state === "online" ? "ok" : a.presence_state === "degraded" ? "partial" : "down")} size="mini" />
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </SectionPanel>
  );
}
