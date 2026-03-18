import type { Agent } from "../lib/api";
import { buildAgentModeLabel, buildAgentRuntimeStatus } from "../lib/telemt-runtime-state";

type FleetRuntimeStatusBadgeProps = {
  agent: Agent;
};

export function FleetRuntimeStatusBadge(props: FleetRuntimeStatusBadgeProps) {
  const status = buildAgentRuntimeStatus(props.agent);

  return <span className={statusClassName(status.tone)}>{status.label}</span>;
}

export function FleetRuntimeModeBadge(props: FleetRuntimeStatusBadgeProps) {
  return (
    <span className="rounded-full bg-slate-950 px-3 py-1 text-xs uppercase tracking-[0.22em] text-white">
      {buildAgentModeLabel(props.agent)}
    </span>
  );
}

function statusClassName(tone: "emerald" | "amber" | "rose" | "sky") {
  return {
    emerald: "rounded-full bg-emerald-100 px-3 py-1 text-xs uppercase tracking-[0.22em] text-emerald-800",
    amber: "rounded-full bg-amber-100 px-3 py-1 text-xs uppercase tracking-[0.22em] text-amber-800",
    rose: "rounded-full bg-rose-100 px-3 py-1 text-xs uppercase tracking-[0.22em] text-rose-800",
    sky: "rounded-full bg-sky-100 px-3 py-1 text-xs uppercase tracking-[0.22em] text-sky-800"
  }[tone];
}
