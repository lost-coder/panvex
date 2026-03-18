import type { Agent } from "../lib/api";
import { buildAgentConnectionSummary } from "../lib/telemt-runtime-state";

type FleetRuntimeSummaryProps = {
  agent: Agent;
};

export function FleetRuntimeConnections(props: FleetRuntimeSummaryProps) {
  const summary = buildAgentConnectionSummary(props.agent);

  return (
    <div>
      <div className="font-medium text-slate-950">{summary.primary}</div>
      <div className="mt-1 text-xs text-slate-500">{summary.secondary}</div>
    </div>
  );
}

export function FleetRuntimeDCSummary(props: FleetRuntimeSummaryProps) {
  const coverage = Math.round(props.agent.runtime.dc_coverage_pct);

  return (
    <div>
      <div className="font-medium text-slate-950">{coverage}% coverage</div>
      <div className="mt-1 text-xs text-slate-500">
        {coverage > 0 && coverage < 100 ? "Below target" : "Stable"}
      </div>
    </div>
  );
}

export function FleetRuntimeUpstreamSummary(props: FleetRuntimeSummaryProps) {
  return (
    <div>
      <div className="font-medium text-slate-950">
        {props.agent.runtime.healthy_upstreams}/{props.agent.runtime.total_upstreams || 0} healthy
      </div>
      <div className="mt-1 text-xs text-slate-500">
        {props.agent.runtime.total_upstreams > 0 && props.agent.runtime.healthy_upstreams < props.agent.runtime.total_upstreams ? "Degraded" : "Stable"}
      </div>
    </div>
  );
}
