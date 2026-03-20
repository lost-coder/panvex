import { Link } from "@tanstack/react-router";

import type { Agent } from "../lib/api";
import {
  buildAgentAttentionList,
  buildAgentAttentionReasons,
  buildAgentConnectionSummary,
  buildAgentRuntimeStatus
} from "../lib/telemt-runtime-state";

type TelemtAttentionPanelProps = {
  agents: Agent[];
};

export function TelemtAttentionPanel(props: TelemtAttentionPanelProps) {
  const attentionAgents = buildAgentAttentionList(props.agents).slice(0, 5);

  return (
    <section className="rounded-[32px] border border-white/70 bg-white/85 p-6 shadow-[0_20px_60px_rgba(37,46,68,0.08)]">
      <div className="flex items-start justify-between gap-4">
        <div>
          <p className="text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">Needs attention</p>
          <h3 className="mt-2 text-2xl font-semibold tracking-tight text-slate-950">Nodes that deserve the next look</h3>
        </div>
        <Link to="/fleet" className="text-sm font-medium text-slate-900 underline underline-offset-4">
          Open fleet
        </Link>
      </div>

      <div className="mt-6 space-y-3">
        {attentionAgents.length > 0 ? (
          attentionAgents.map((agent) => {
            const status = buildAgentRuntimeStatus(agent);
            const connections = buildAgentConnectionSummary(agent);
            const reasons = buildAgentAttentionReasons(agent);

            return (
              <div key={agent.id} className="rounded-3xl border border-slate-200 bg-slate-50 p-4">
                <div className="flex items-start justify-between gap-4">
                  <div>
                    <p className="font-medium text-slate-950">{agent.node_name}</p>
                    <p className="mt-1 text-sm text-slate-600">{agent.runtime.transport_mode || "unknown"} mode in {agent.fleet_group_id || "Ungrouped"}</p>
                  </div>
                  <span className={statusClassName(status.tone)}>{status.label}</span>
                </div>
                {reasons.length > 0 ? (
                  <div className="mt-4 flex flex-wrap gap-2">
                    {reasons.map((reason) => (
                      <span key={reason.label} className={reasonClassName(reason.tone)}>
                        {reason.label}
                      </span>
                    ))}
                  </div>
                ) : null}
                <div className="mt-4 grid gap-3 text-sm text-slate-600 sm:grid-cols-3">
                  <div>
                    <p className="text-xs font-semibold uppercase tracking-[0.22em] text-slate-500">Connections</p>
                    <p className="mt-2 font-medium text-slate-950">{connections.primary}</p>
                    <p className="mt-1 text-xs text-slate-500">{connections.secondary}</p>
                  </div>
                  <div>
                    <p className="text-xs font-semibold uppercase tracking-[0.22em] text-slate-500">DC coverage</p>
                    <p className="mt-2 font-medium text-slate-950">{Math.round(agent.runtime.dc_coverage_pct)}%</p>
                  </div>
                  <div>
                    <p className="text-xs font-semibold uppercase tracking-[0.22em] text-slate-500">Upstreams</p>
                    <p className="mt-2 font-medium text-slate-950">
                      {agent.runtime.healthy_upstreams}/{agent.runtime.total_upstreams || 0} healthy
                    </p>
                  </div>
                </div>
                <div className="mt-4 flex justify-end">
                  <Link
                    to="/fleet/$agentId"
                    params={{ agentId: agent.id }}
                    className="inline-flex rounded-2xl border border-slate-200 bg-white px-4 py-2.5 text-sm font-medium text-slate-700 transition hover:border-slate-300 hover:text-slate-950"
                  >
                    Open node
                  </Link>
                </div>
              </div>
            );
          })
        ) : (
          <div className="rounded-[28px] border border-dashed border-slate-300 bg-slate-50/80 px-5 py-10 text-center">
            <h4 className="text-lg font-semibold text-slate-950">Nothing urgent right now</h4>
            <p className="mt-3 text-sm leading-6 text-slate-600">
              Once a node goes offline or starts degrading, it will show up here first.
            </p>
          </div>
        )}
      </div>
    </section>
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

function reasonClassName(tone: "emerald" | "amber" | "rose") {
  return {
    emerald: "rounded-full bg-emerald-100 px-3 py-1 text-xs font-semibold uppercase tracking-[0.18em] text-emerald-800",
    amber: "rounded-full bg-amber-100 px-3 py-1 text-xs font-semibold uppercase tracking-[0.18em] text-amber-800",
    rose: "rounded-full bg-rose-100 px-3 py-1 text-xs font-semibold uppercase tracking-[0.18em] text-rose-800"
  }[tone];
}
