import type { ControlRoomResponse } from "../lib/api";

type ControlRoomStatusStripProps = {
  summary: ControlRoomResponse;
};

export function ControlRoomStatusStrip(props: ControlRoomStatusStripProps) {
  const healthyNodes = Math.max(props.summary.fleet.online_agents - props.summary.fleet.degraded_agents, 0);
  const metrics = [
    { label: "Healthy", value: String(healthyNodes), tone: "emerald" as const },
    { label: "Degraded", value: String(props.summary.fleet.degraded_agents), tone: "amber" as const },
    { label: "Offline", value: String(props.summary.fleet.offline_agents), tone: "rose" as const },
    { label: "Live", value: String(props.summary.fleet.live_connections), tone: "slate" as const },
    { label: "Accepting", value: String(props.summary.fleet.accepting_new_connections_agents), tone: "sky" as const },
    { label: "Middle", value: String(props.summary.fleet.middle_proxy_agents), tone: "slate" as const },
    { label: "DC issues", value: String(props.summary.fleet.dc_issue_agents), tone: "amber" as const }
  ];

  return (
    <section className="rounded-[32px] border border-white/70 bg-white/85 p-4 shadow-[0_20px_60px_rgba(37,46,68,0.08)]">
      <div className="overflow-x-auto">
        <div className="grid min-w-[860px] grid-cols-7 gap-3">
          {metrics.map((metric) => (
            <div
              key={metric.label}
              className={`rounded-[24px] border px-4 py-4 ${toneClassName(metric.tone)}`}
            >
              <p className="text-xs font-semibold uppercase tracking-[0.22em] text-slate-500">{metric.label}</p>
              <p className="mt-3 text-3xl font-semibold tracking-tight text-slate-950">{metric.value}</p>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}

function toneClassName(tone: "emerald" | "amber" | "rose" | "sky" | "slate") {
  return {
    emerald: "border-emerald-200 bg-emerald-50/70",
    amber: "border-amber-200 bg-amber-50/70",
    rose: "border-rose-200 bg-rose-50/70",
    sky: "border-sky-200 bg-sky-50/70",
    slate: "border-slate-200 bg-slate-50"
  }[tone];
}
