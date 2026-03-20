import { Link } from "@tanstack/react-router";

import type { Agent } from "../lib/api";
import {
  buildAgentConnectionSummary,
  buildAgentDCIssueSummary,
  buildAgentModeLabel,
  buildAgentRuntimeStatus
} from "../lib/telemt-runtime-state";

type FleetNodeCardGridProps = {
  agents: Agent[];
};

export function FleetNodeCardGrid(props: FleetNodeCardGridProps) {
  return (
    <section className="app-card rounded-[32px]">
      <div className="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
        <div>
          <p className="text-xs font-semibold uppercase tracking-[0.24em] text-[var(--app-text-tertiary)]">All nodes</p>
          <h3 className="mt-2 text-2xl font-semibold tracking-tight text-[var(--app-text-primary)]">Every node at a glance</h3>
          <p className="mt-3 max-w-2xl text-sm leading-6 text-[var(--app-text-secondary)]">
            Scan the whole fleet quickly, then open the node page that needs the next operator decision.
          </p>
        </div>
        <Link to="/fleet" className="text-sm font-medium text-[var(--app-text-primary)] underline underline-offset-4">
          Open fleet table
        </Link>
      </div>

      <div className="mt-6 grid gap-4 md:grid-cols-2 xl:grid-cols-3">
        {props.agents.map((agent) => (
          <FleetNodeCard key={agent.id} agent={agent} />
        ))}
      </div>
    </section>
  );
}

function FleetNodeCard(props: { agent: Agent }) {
  const status = buildAgentRuntimeStatus(props.agent);
  const connections = buildAgentConnectionSummary(props.agent);
  const dcSummary = buildAgentDCIssueSummary(props.agent);

  return (
    <article className="app-card-muted rounded-[28px] p-5 shadow-[0_16px_36px_rgba(37,46,68,0.06)]">
      <div className="flex items-start justify-between gap-4">
        <div>
          <p className="text-lg font-semibold tracking-tight text-[var(--app-text-primary)]">{props.agent.node_name}</p>
          <p className="mt-1 text-sm text-[var(--app-text-tertiary)]">{props.agent.fleet_group_id || "Ungrouped"}</p>
        </div>
        <span className={statusClassName(status.tone)}>{status.label}</span>
      </div>

      <div className="mt-5 grid gap-4 lg:grid-cols-[minmax(0,1.05fr),1fr]">
        <DCIssueSummaryPanel summary={dcSummary} />
        <div className="grid gap-3 sm:grid-cols-2">
          <MetricStack label="Users" value={String(props.agent.runtime.active_users)} hint={connections.primary + " live"} />
          <MetricStack label="Mode" value={buildAgentModeLabel(props.agent)} hint={connections.secondary} />
          <MetricStack
            label="DC coverage"
            value={`${Math.round(props.agent.runtime.dc_coverage_pct)}%`}
            hint={dcSummary.totalCount > 0 ? `${dcSummary.okCount} OK / ${dcSummary.issueCount} issues` : "Awaiting DC data"}
          />
          <MetricStack
            label="Upstreams"
            value={`${props.agent.runtime.healthy_upstreams}/${props.agent.runtime.total_upstreams || 0}`}
            hint="healthy"
          />
        </div>
      </div>

      <div className="mt-5 flex items-center justify-between gap-4">
        <p className="text-xs uppercase tracking-[0.22em] text-[var(--app-text-tertiary)]">
          Last seen {new Date(props.agent.last_seen_at).toLocaleString()}
        </p>
        <Link
          to="/fleet/$agentId"
          params={{ agentId: props.agent.id }}
          className="app-button-secondary inline-flex rounded-2xl px-4 py-2.5 text-sm font-medium"
        >
          View details
        </Link>
      </div>
    </article>
  );
}

function DCIssueSummaryPanel(props: {
  summary: {
    totalCount: number;
    okCount: number;
    issueCount: number;
    issues: Array<{
      dc: number;
      label: string;
      stateLabel: "Reduced" | "Limited" | "Down";
      tone: "sky" | "amber" | "rose";
    }>;
  };
}) {
  if (props.summary.totalCount === 0) {
    return (
      <section className="app-card-muted rounded-[24px] border-dashed p-4">
        <p className="text-xs font-semibold uppercase tracking-[0.22em] text-[var(--app-text-tertiary)]">DC state</p>
        <p className="mt-3 text-lg font-semibold tracking-tight text-[var(--app-text-primary)]">No DC data yet</p>
        <p className="mt-2 text-sm leading-6 text-[var(--app-text-tertiary)]">This node has not reported any per-DC coverage details yet.</p>
      </section>
    );
  }

  const visibleIssues = props.summary.issues.slice(0, 3);
  const hiddenIssues = Math.max(props.summary.issueCount - visibleIssues.length, 0);

  return (
    <section className="app-card-muted rounded-[24px] p-4">
      <p className="text-xs font-semibold uppercase tracking-[0.22em] text-[var(--app-text-tertiary)]">DC state</p>
      <div className="mt-3 flex flex-wrap items-baseline gap-x-3 gap-y-1">
        <span className="text-3xl font-semibold tracking-tight text-emerald-700">{props.summary.okCount} OK</span>
        <span className={props.summary.issueCount > 0 ? "text-3xl font-semibold tracking-tight text-amber-700" : "text-3xl font-semibold tracking-tight text-slate-400"}>
          {props.summary.issueCount} issues
        </span>
      </div>
      <p className="mt-2 text-xs uppercase tracking-[0.22em] text-[var(--app-text-tertiary)]">{props.summary.totalCount} DC total</p>

      <div className="mt-4 flex flex-wrap gap-2">
        {visibleIssues.length > 0 ? (
          visibleIssues.map((issue) => (
            <span key={issue.dc} className={issueClassName(issue.tone)}>
              {issue.label}
            </span>
          ))
        ) : (
          <span className="rounded-full bg-emerald-100 px-3 py-1.5 text-xs font-semibold uppercase tracking-[0.18em] text-emerald-800">
            All DCs stable
          </span>
        )}
        {hiddenIssues > 0 ? (
          <span className="rounded-full bg-slate-200 px-3 py-1.5 text-xs font-semibold uppercase tracking-[0.18em] text-slate-700">
            +{hiddenIssues} more
          </span>
        ) : null}
      </div>
    </section>
  );
}

function MetricStack(props: { label: string; value: string; hint: string }) {
  return (
    <div>
      <p className="text-xs font-semibold uppercase tracking-[0.22em] text-[var(--app-text-tertiary)]">{props.label}</p>
      <p className="mt-2 text-lg font-semibold tracking-tight text-[var(--app-text-primary)]">{props.value}</p>
      <p className="mt-1 text-xs text-[var(--app-text-tertiary)]">{props.hint}</p>
    </div>
  );
}

function statusClassName(tone: "emerald" | "amber" | "rose" | "sky") {
  return {
    emerald: "rounded-full bg-emerald-100 px-3 py-1 text-xs font-semibold uppercase tracking-[0.22em] text-emerald-800",
    amber: "rounded-full bg-amber-100 px-3 py-1 text-xs font-semibold uppercase tracking-[0.22em] text-amber-800",
    rose: "rounded-full bg-rose-100 px-3 py-1 text-xs font-semibold uppercase tracking-[0.22em] text-rose-800",
    sky: "rounded-full bg-sky-100 px-3 py-1 text-xs font-semibold uppercase tracking-[0.22em] text-sky-800"
  }[tone];
}

function issueClassName(tone: "sky" | "amber" | "rose") {
  return {
    sky: "rounded-full bg-sky-100 px-3 py-1.5 text-xs font-semibold uppercase tracking-[0.18em] text-sky-800",
    amber: "rounded-full bg-amber-100 px-3 py-1.5 text-xs font-semibold uppercase tracking-[0.18em] text-amber-800",
    rose: "rounded-full bg-rose-100 px-3 py-1.5 text-xs font-semibold uppercase tracking-[0.18em] text-rose-800"
  }[tone];
}
