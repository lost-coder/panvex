import { SummaryStatsRow } from "@/components/summary-stats-row";
import { useAppearanceSettings } from "@/features/profile/profile-state";
import { ActivityPanel } from "./activity-panel";
import { DashboardSummaryCard } from "./dashboard-summary-card";
import { ServerCard } from "./server-card";
import { useDashboardClients, useDashboardData } from "./dashboard-state";
import {
  buildFleetDcCoverageSummary,
  buildFleetKpiSummary,
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
  const dashboardQuery = useDashboardData();
  const clientsQuery = useDashboardClients();
  const appearanceQuery = useAppearanceSettings();
  const dashboard = dashboardQuery.data;
  const agents = dashboard?.server_cards.map((item) => item.agent) ?? [];
  const helpMode = appearanceQuery.data?.help_mode ?? "basic";
  const showHelp = helpMode !== "off";

  if (dashboardQuery.isLoading || clientsQuery.isLoading) {
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

  if (dashboardQuery.isError || clientsQuery.isError) {
    return (
      <div className="rounded border border-bad/30 bg-bad-dim px-4 py-3 text-sm font-semibold text-bad-text">
        Dashboard data is unavailable.
      </div>
    );
  }

  const kpiSummary = buildFleetKpiSummary(dashboard, agents, clientsQuery.data ?? []);
  const dcSummary = buildFleetDcCoverageSummary(agents);

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
        <DashboardSectionHeading title="Attention Queue" />
        {showHelp ? (
          <div className="rounded border border-border bg-surface px-3 py-2 text-sm text-text-3">
            The attention queue surfaces the nodes that need operator action first. Cards below stay available for the familiar per-server DC scan.
          </div>
        ) : null}
        <div className="grid gap-3 lg:grid-cols-[2fr_1fr]">
          <div className="rounded border border-border bg-surface p-4">
            {dashboard?.attention.length ? (
              <div className="space-y-3">
                {dashboard.attention.map((item) => (
                  <div key={item.agent_id} className="flex items-start justify-between gap-4 border-b border-border pb-3 last:border-0 last:pb-0">
                    <div>
                      <div className="text-sm font-semibold text-text-1">{item.node_name}</div>
                      <div className="text-xs text-text-3">{item.fleet_group_id || "Ungrouped"}</div>
                      <div className="mt-1 text-sm text-text-2">{item.reason}</div>
                    </div>
                    <div className="shrink-0 text-right">
                      <div className="text-xs font-semibold uppercase tracking-[0.12em] text-text-3">{item.severity}</div>
                      <div className="mt-1 text-xs text-text-3">{item.runtime_freshness.state}</div>
                    </div>
                  </div>
                ))}
              </div>
            ) : (
              <div className="text-sm text-text-3">No servers currently need operator attention.</div>
            )}
          </div>
          <div className="rounded border border-border bg-surface p-4">
            <div className="text-sm font-semibold text-text-1">Runtime Distribution</div>
            <div className="mt-3 space-y-2">
              {Object.entries(dashboard?.runtime_distribution ?? {}).length > 0 ? (
                Object.entries(dashboard?.runtime_distribution ?? {}).map(([mode, count]) => (
                  <div key={mode} className="flex items-center justify-between text-sm">
                    <span className="text-text-2">{mode.replaceAll("_", " ")}</span>
                    <span className="font-mono text-text-1">{count}</span>
                  </div>
                ))
              ) : (
                <div className="text-sm text-text-3">No runtime distribution data yet.</div>
              )}
            </div>
          </div>
        </div>
      </section>
      <section className="space-y-3">
        <DashboardSectionHeading title="Servers" />
        <div
          className="grid gap-3"
          data-slot="server-grid"
          style={{ gridTemplateColumns: "repeat(auto-fill, minmax(340px, 1fr))" }}
        >
          {(dashboard?.server_cards ?? []).map((item) => (
            <ServerCard key={item.agent.id} helpMode={helpMode} item={item} />
          ))}
        </div>
      </section>
      <section className="space-y-3">
        <DashboardSectionHeading title="Operational Context" />
        <div className="grid lg:grid-cols-2 gap-4">
          <div className="rounded border border-border bg-surface p-4">
            <div className="text-sm font-semibold text-text-1">DC Degradation</div>
            <div className="mt-3 space-y-2">
              <div className="flex items-center justify-between text-sm">
                <span className="text-text-2">Tracked DCs</span>
                <span className="font-mono text-text-1">{dcSummary.totalDcCount}</span>
              </div>
              <div className="flex items-center justify-between text-sm">
                <span className="text-good-text">OK</span>
                <span className="font-mono text-text-1">{dcSummary.okCount}</span>
              </div>
              <div className="flex items-center justify-between text-sm">
                <span className="text-warn-text">Partial</span>
                <span className="font-mono text-text-1">{dcSummary.partialCount}</span>
              </div>
              <div className="flex items-center justify-between text-sm">
                <span className="text-bad-text">Down</span>
                <span className="font-mono text-text-1">{dcSummary.downCount}</span>
              </div>
            </div>
          </div>
          <ActivityPanel agents={agents} />
        </div>
      </section>
    </div>
  );
}
