import type { ControlRoomResponse } from "../lib/api";

type TelemtRuntimeDistributionProps = {
  summary: ControlRoomResponse;
};

export function TelemtRuntimeDistribution(props: TelemtRuntimeDistributionProps) {
  return (
    <section className="rounded-[32px] border border-white/70 bg-white/85 p-6 shadow-[0_20px_60px_rgba(37,46,68,0.08)]">
      <p className="text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">Runtime distribution</p>
      <h3 className="mt-2 text-2xl font-semibold tracking-tight text-slate-950">Where the fleet is spending effort</h3>

      <div className="mt-6 grid gap-4 sm:grid-cols-2">
        <DistributionCard
          label="Live connections"
          value={String(props.summary.fleet.live_connections)}
          description="Current MTProto sessions observed across the connected nodes."
        />
        <DistributionCard
          label="Accepting new connections"
          value={String(props.summary.fleet.accepting_new_connections_agents)}
          description="Nodes that are still open for new sessions right now."
        />
        <DistributionCard
          label="Middle-proxy nodes"
          value={String(props.summary.fleet.middle_proxy_agents)}
          description="Nodes currently preferring the middle-proxy transport path."
        />
        <DistributionCard
          label="DC issues"
          value={String(props.summary.fleet.dc_issue_agents)}
          description="Nodes reporting reduced DC coverage and likely path degradation."
        />
      </div>
    </section>
  );
}

function DistributionCard(props: { label: string; value: string; description: string }) {
  return (
    <div className="rounded-3xl border border-slate-200 bg-slate-50 p-4">
      <p className="text-xs font-semibold uppercase tracking-[0.22em] text-slate-500">{props.label}</p>
      <p className="mt-4 text-3xl font-semibold tracking-tight text-slate-950">{props.value}</p>
      <p className="mt-3 text-sm leading-6 text-slate-600">{props.description}</p>
    </div>
  );
}
