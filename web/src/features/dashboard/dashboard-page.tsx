import { SummaryStatsRow } from "@/components/summary-stats-row";
import { ActivityPanel } from "./activity-panel";
import { DashboardSummaryCard } from "./dashboard-summary-card";
import { DcOverviewPanel } from "./dc-overview-panel";
import { ServerCard } from "./server-card";
import { useDashboardClients, useDashboardData, useAgentsList } from "./dashboard-state";
import {
  buildFleetDcCoverageSummary,
  buildFleetKpiSummary,
  sortAgentsBySeverity,
} from "./dashboard-view-model";

function formatTrafficBytes(bytes: number): string {
  const units = ["B", "KB", "MB", "GB", "TB"];
  let value = bytes;
  let unitIndex = 0;

  while (value >= 1024 && unitIndex < units.length - 1) {
    value /= 1024;
    unitIndex += 1;
  }

  if (unitIndex === 0) {
    return `${Math.round(value)} ${units[unitIndex]}`;
  }

  return `${value.toFixed(1)} ${units[unitIndex]}`;
}

function DashboardSectionHeading({ title }: { title: string }) {
  return (
    <div className="flex items-center gap-3">
      <span className="shrink-0 text-[11px] font-bold uppercase tracking-[0.22em] text-text-3">
        {title}
      </span>
      <div
        aria-hidden="true"
        className="h-px flex-1 bg-linear-to-r from-border-active/50 via-border to-transparent"
      />
    </div>
  );
}

export function DashboardPage() {
  const controlRoomQuery = useDashboardData();
  const agentsQuery = useAgentsList();
  const clientsQuery = useDashboardClients();
  const agents = agentsQuery.data ?? [];

  if (controlRoomQuery.isLoading || agentsQuery.isLoading || clientsQuery.isLoading) {
    return (
      <div className="space-y-3">
        <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
          {[...Array(4)].map((_, index) => (
            <div key={index} className="animate-pulse bg-surface h-24 rounded" />
          ))}
        </div>
      </div>
    );
  }

  if (controlRoomQuery.isError || agentsQuery.isError || clientsQuery.isError) {
    return (
      <div className="rounded border border-bad/30 bg-bad-dim px-4 py-3 text-sm font-semibold text-bad-text">
        Dashboard data is unavailable.
      </div>
    );
  }

  const kpiSummary = buildFleetKpiSummary(controlRoomQuery.data, agents, clientsQuery.data ?? []);
  const dcSummary = buildFleetDcCoverageSummary(agents);
  const sortedAgents = sortAgentsBySeverity(agents);

  return (
    <div className="space-y-6">
      <section className="space-y-3">
        <DashboardSectionHeading title="Fleet Overview" />
        <SummaryStatsRow>
          <DashboardSummaryCard
            label="Servers"
            value={kpiSummary.totalServers}
            breakdownItems={[
              { label: "Online", value: kpiSummary.onlineServers, tone: "good" },
              { label: "Degraded", value: kpiSummary.degradedServers, tone: "warn" },
              { label: "Offline", value: kpiSummary.offlineServers, tone: "bad" },
            ]}
          />
          <DashboardSummaryCard
            label="Clients / Active Connections"
            value={kpiSummary.totalClients}
            secondaryText={`${kpiSummary.activeConnections} active connections`}
          />
          <DashboardSummaryCard
            label="Traffic"
            value={formatTrafficBytes(kpiSummary.totalTrafficBytes)}
          />
          <DashboardSummaryCard
            label="DC Coverage"
            value={`${kpiSummary.dcCoveragePct}%`}
            secondaryText={`${dcSummary.totalDcCount} DCs tracked`}
            breakdownItems={[
              { label: "OK", value: dcSummary.okCount, tone: "good" },
              { label: "Partial", value: dcSummary.partialCount, tone: "warn" },
              { label: "Down", value: dcSummary.downCount, tone: "bad" },
            ]}
          />
        </SummaryStatsRow>
      </section>
      <section className="space-y-3">
        <DashboardSectionHeading title="Servers" />
        <div
          className="grid gap-3"
          data-slot="server-grid"
          style={{ gridTemplateColumns: "repeat(auto-fill, minmax(340px, 1fr))" }}
        >
          {sortedAgents.map((agent) => (
            <ServerCard key={agent.id} agent={agent} />
          ))}
        </div>
      </section>
      <section className="space-y-3">
        <DashboardSectionHeading title="Operational Context" />
        <div className="grid lg:grid-cols-2 gap-4">
          <DcOverviewPanel agents={agents} />
          <ActivityPanel agents={agents} />
        </div>
      </section>
    </div>
  );
}
