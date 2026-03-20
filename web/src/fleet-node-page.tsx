import { Link, useParams } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";

import { FleetRuntimeModeBadge, FleetRuntimeStatusBadge } from "./components/fleet-runtime-status-badge";
import { apiClient } from "./lib/api";
import {
  buildAgentConnectionSummary,
  buildAgentDCCoverageStage,
  buildAgentModeLabel
} from "./lib/telemt-runtime-state";

export function FleetNodePage() {
  const { agentId } = useParams({ from: "/shell/fleet/$agentId" });
  const agentsQuery = useQuery({ queryKey: ["agents"], queryFn: () => apiClient.agents() });
  const instancesQuery = useQuery({ queryKey: ["instances"], queryFn: () => apiClient.instances() });

  if (agentsQuery.isLoading || instancesQuery.isLoading) {
    return <CenteredMessage title="Loading node" description="Pulling together the latest Telemt snapshot for the selected node." />;
  }

  if (agentsQuery.isError || instancesQuery.isError) {
    return <CenteredMessage title="Node is unavailable" description="The control-plane could not load the selected node." />;
  }

  const agent = (agentsQuery.data ?? []).find((candidate) => candidate.id === agentId);
  if (!agent) {
    return <CenteredMessage title="Node not found" description="This node is no longer present in the current fleet inventory." />;
  }

  const scopedInstances = (instancesQuery.data ?? []).filter((instance) => instance.agent_id === agent.id);
  const connections = buildAgentConnectionSummary(agent);
  const dcStage = buildAgentDCCoverageStage(agent);

  return (
    <div className="space-y-6">
      <section className="app-hero-panel rounded-[36px] p-6 lg:p-8">
        <div className="flex flex-col gap-5 xl:flex-row xl:items-start xl:justify-between">
          <div className="max-w-3xl">
            <Link to="/fleet" className="text-xs font-semibold uppercase tracking-[0.24em] text-slate-500 underline underline-offset-4">
              Back to fleet
            </Link>
            <h2 className="mt-4 text-4xl font-semibold tracking-tight text-slate-950">{agent.node_name}</h2>
            <p className="mt-2 text-sm text-slate-600">{agent.id}</p>
            <p className="mt-4 max-w-2xl text-sm leading-7 text-slate-600">
              This is the dedicated node page stub. The top-level runtime summary is live already, while the deep Telemt diagnostic sections are being expanded next.
            </p>
          </div>

          <div className="flex flex-wrap gap-3">
            <FleetRuntimeStatusBadge agent={agent} />
            <FleetRuntimeModeBadge agent={agent} />
          </div>
        </div>

        <div className="mt-6 grid gap-4 md:grid-cols-2 xl:grid-cols-4">
          <MetricCard label="Group" value={agent.fleet_group_id || "Ungrouped"} hint={`Mode ${buildAgentModeLabel(agent)}`} />
          <MetricCard label="Users" value={String(agent.runtime.active_users)} hint={`${connections.primary} live connections`} />
          <MetricCard label="DC coverage" value={`${Math.round(agent.runtime.dc_coverage_pct)}%`} hint={`Stage ${dcStage}%`} />
          <MetricCard
            label="Upstreams"
            value={`${agent.runtime.healthy_upstreams}/${agent.runtime.total_upstreams || 0}`}
            hint="healthy right now"
          />
        </div>
      </section>

      <section className="rounded-[32px] border border-white/70 bg-white/85 p-6 shadow-[0_20px_60px_rgba(37,46,68,0.08)]">
        <p className="text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">Current runtime summary</p>
        <div className="mt-5 grid gap-4 md:grid-cols-2 xl:grid-cols-4">
          <MetricCard
            label="Admissions"
            value={agent.runtime.accepting_new_connections ? "Open" : "Paused"}
            hint="new connections"
          />
          <MetricCard
            label="Fallback"
            value={agent.runtime.me2dc_fallback_enabled ? "Enabled" : "Disabled"}
            hint="middle-proxy to direct"
          />
          <MetricCard
            label="Startup"
            value={agent.runtime.startup_status || "unknown"}
            hint={agent.runtime.startup_stage || "no stage"}
          />
          <MetricCard
            label="Last seen"
            value={new Date(agent.last_seen_at).toLocaleString()}
            hint={`${scopedInstances.length} Telemt instance${scopedInstances.length === 1 ? "" : "s"}`}
          />
        </div>
      </section>

      <section className="grid gap-6 xl:grid-cols-2">
        <PlaceholderSection
          title="DC health"
          description="Per-DC coverage, writers, endpoint health, and latency will move here from the fleet drawer next."
        />
        <PlaceholderSection
          title="Upstreams"
          description="Detailed upstream rows, route kinds, and latency history will become the second diagnostic section."
        />
        <PlaceholderSection
          title="Users"
          description="Per-user sessions, throughput, and IP intelligence will land here after the Telemt-side data model is finalized."
        />
        <PlaceholderSection
          title="Recent runtime events"
          description="Recent node-local Telemt events will graduate from the dashboard into a dedicated timeline here."
        />
      </section>

      <section className="app-empty-state rounded-[32px] p-6">
        <p className="text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">Diagnostics</p>
        <h3 className="mt-2 text-2xl font-semibold tracking-tight text-slate-950">Deep runtime diagnostics are coming next</h3>
        <p className="mt-3 max-w-3xl text-sm leading-6 text-slate-600">
          This page is intentionally a stub for now. The next step is to move the rich Telemt detail out of the fleet drawer and turn this route into the canonical node workspace.
        </p>
      </section>
    </div>
  );
}

function MetricCard(props: { label: string; value: string; hint: string }) {
  return (
    <div className="rounded-3xl border border-slate-200 bg-slate-50 px-4 py-5">
      <p className="text-xs font-semibold uppercase tracking-[0.22em] text-slate-500">{props.label}</p>
      <p className="mt-3 text-2xl font-semibold tracking-tight text-slate-950">{props.value}</p>
      <p className="mt-2 text-sm text-slate-500">{props.hint}</p>
    </div>
  );
}

function PlaceholderSection(props: { title: string; description: string }) {
  return (
    <section className="rounded-[32px] border border-white/70 bg-white/85 p-6 shadow-[0_20px_60px_rgba(37,46,68,0.08)]">
      <p className="text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">{props.title}</p>
      <h3 className="mt-2 text-2xl font-semibold tracking-tight text-slate-950">Section scaffold</h3>
      <p className="mt-3 text-sm leading-6 text-slate-600">{props.description}</p>
    </section>
  );
}

function CenteredMessage(props: { title: string; description: string }) {
  return (
    <div className="flex min-h-[50vh] items-center justify-center">
      <div className="app-panel rounded-[32px] max-w-lg p-8 text-center">
        <h3 className="text-2xl font-semibold tracking-tight text-slate-950">{props.title}</h3>
        <p className="mt-3 text-sm text-slate-600">{props.description}</p>
      </div>
    </div>
  );
}
