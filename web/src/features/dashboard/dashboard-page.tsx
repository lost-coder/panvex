import { SummaryStatsRow } from "@/components/summary-stats-row";
import { StatCard } from "@/components/ui/stat-card";
import { ActivityPanel } from "./activity-panel";
import { DcOverviewPanel } from "./dc-overview-panel";
import { ServerCard } from "./server-card";
import { useDashboardData, useAgentsList } from "./dashboard-state";

export function DashboardPage() {
  const { data: controlRoom, isLoading } = useDashboardData();
  const { data: agents = [] } = useAgentsList();
  const fleet = controlRoom?.fleet;

  if (isLoading) {
    return (
      <div className="space-y-3">
        <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
          {[...Array(4)].map((_, i) => (
            <div key={i} className="animate-pulse bg-surface h-20 rounded" />
          ))}
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-3">
      <SummaryStatsRow>
        <StatCard label="Servers" value={fleet?.total_agents ?? 0} />
        <StatCard label="Clients" value={fleet?.live_connections ?? 0} />
        <StatCard label="Online" value={fleet?.online_agents ?? 0} />
        <StatCard label="Offline" value={fleet?.offline_agents ?? 0} />
      </SummaryStatsRow>
      <div className="grid lg:grid-cols-2 gap-4">
        <DcOverviewPanel agents={agents} />
        <ActivityPanel agents={agents} />
      </div>
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3">
        {agents.map((agent: any) => (
          <ServerCard key={agent.id} agent={agent} />
        ))}
      </div>
    </div>
  );
}
