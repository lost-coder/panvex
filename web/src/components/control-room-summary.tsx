import type { ControlRoomResponse } from "../lib/api";

type ControlRoomSummaryProps = {
  summary: ControlRoomResponse;
};

export function ControlRoomSummary(props: ControlRoomSummaryProps) {
  const needsAttention = props.summary.fleet.degraded_agents + props.summary.fleet.offline_agents;

  return (
    <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
      <SummaryCard
        eyebrow="Servers online"
        value={String(props.summary.fleet.online_agents)}
        description={props.summary.fleet.total_agents === 1 ? "The connected server is reporting in." : "Servers with a healthy heartbeat right now."}
        tone="emerald"
      />
      <SummaryCard
        eyebrow="Needs attention"
        value={String(needsAttention)}
        description={needsAttention === 0 ? "Nothing looks stale at the moment." : "Servers that are degraded or offline."}
        tone="amber"
      />
      <SummaryCard
        eyebrow="Telemt runtimes"
        value={String(props.summary.fleet.total_instances)}
        description="Connected Telemt instances discovered through the agent layer."
        tone="sky"
      />
      <SummaryCard
        eyebrow="Failed actions"
        value={String(props.summary.jobs.failed)}
        description={props.summary.jobs.failed === 0 ? "No recent failed actions." : "Recent jobs that need a second look."}
        tone="rose"
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
