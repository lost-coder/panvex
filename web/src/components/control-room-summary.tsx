import type { ControlRoomResponse } from "../lib/api";

type ControlRoomSummaryProps = {
  summary: ControlRoomResponse;
};

export function ControlRoomSummary(props: ControlRoomSummaryProps) {
  const healthyNodes = Math.max(props.summary.fleet.online_agents - props.summary.fleet.degraded_agents, 0);

  return (
    <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
      <SummaryCard
        eyebrow="Healthy nodes"
        value={String(healthyNodes)}
        description={healthyNodes === 1 ? "One node looks healthy right now." : "Nodes that are online without degraded runtime signals."}
        tone="emerald"
      />
      <SummaryCard
        eyebrow="Degraded nodes"
        value={String(props.summary.fleet.degraded_agents)}
        description={props.summary.fleet.degraded_agents === 0 ? "No degraded runtime signals are active." : "Nodes that are online but need operator attention."}
        tone="amber"
      />
      <SummaryCard
        eyebrow="Offline nodes"
        value={String(props.summary.fleet.offline_agents)}
        description={props.summary.fleet.offline_agents === 0 ? "Nothing is currently missing from the fleet." : "Nodes that stopped reporting to the control plane."}
        tone="rose"
      />
      <SummaryCard
        eyebrow="Live connections"
        value={String(props.summary.fleet.live_connections)}
        description="Current MTProto sessions observed across all connected nodes."
        tone="sky"
      />
    </div>
  );
}

function SummaryCard(props: {
  eyebrow: string;
  value: string;
  description: string;
  tone: "emerald" | "amber" | "sky" | "rose";
}) {
  const toneClass = {
    emerald: "from-emerald-500/18 to-emerald-200/10",
    amber: "from-amber-500/18 to-amber-200/10",
    sky: "from-sky-500/18 to-sky-200/10",
    rose: "from-rose-500/18 to-rose-200/10"
  }[props.tone];

  return (
    <div className={`rounded-[28px] border border-white/80 bg-gradient-to-br ${toneClass} p-5 shadow-[0_20px_60px_rgba(37,46,68,0.08)]`}>
      <p className="text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">{props.eyebrow}</p>
      <p className="mt-5 text-4xl font-semibold tracking-tight text-slate-950">{props.value}</p>
      <p className="mt-3 text-sm leading-6 text-slate-600">{props.description}</p>
    </div>
  );
}
