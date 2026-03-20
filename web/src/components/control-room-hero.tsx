import { Link } from "@tanstack/react-router";

import type { ControlRoomResponse } from "../lib/api";

type ControlRoomHeroProps = {
  summary: ControlRoomResponse;
  onAddNode: () => void;
};

export function ControlRoomHero(props: ControlRoomHeroProps) {
  const headline = describeHeadline(props.summary);
  const statusTone =
    props.summary.fleet.offline_agents > 0
      ? "bg-rose-100 text-rose-800"
      : props.summary.fleet.degraded_agents > 0
        ? "bg-amber-100 text-amber-800"
        : "bg-emerald-100 text-emerald-800";
  const statusLabel =
    props.summary.onboarding.needs_first_server
      ? "Ready for first connection"
      : props.summary.fleet.offline_agents > 0
        ? "Some servers need attention"
        : props.summary.fleet.degraded_agents > 0
          ? "A few servers look stale"
          : "Everything is reporting in";

  return (
    <section className="app-card relative overflow-hidden rounded-[36px] lg:p-8">
      <div className="absolute -right-10 top-0 h-40 w-40 rounded-full bg-sky-300/20 blur-3xl" />
      <div className="absolute bottom-0 left-0 h-32 w-32 rounded-full bg-emerald-300/20 blur-3xl" />
      <div className="relative flex flex-col gap-6 xl:flex-row xl:items-end xl:justify-between">
        <div className="max-w-3xl">
          <p className="text-xs font-semibold uppercase tracking-[0.28em] text-[var(--app-text-tertiary)]">Panvex</p>
          <div className="mt-4 flex flex-wrap items-center gap-3">
            <h2 className="text-4xl font-semibold tracking-tight text-[var(--app-text-primary)] lg:text-5xl">Control Room</h2>
            <span className={`rounded-full px-3 py-1 text-xs font-semibold uppercase tracking-[0.22em] ${statusTone}`}>
              {statusLabel}
            </span>
          </div>
          <p className="mt-4 max-w-2xl text-sm leading-7 text-[var(--app-text-secondary)] lg:text-base">{headline}</p>
        </div>

        <div className="grid gap-3 sm:grid-cols-3 xl:min-w-[420px]">
          <HeroPill label="Online now" value={String(props.summary.fleet.online_agents)} />
          <HeroPill label="Live connections" value={String(props.summary.fleet.live_connections)} />
          <HeroPill label="Middle nodes" value={String(props.summary.fleet.middle_proxy_agents)} />
        </div>
      </div>

      <div className="relative mt-6 flex flex-wrap gap-3">
        <button
          type="button"
          className="app-button-primary inline-flex rounded-2xl text-sm font-medium"
          onClick={props.onAddNode}
        >
          Add node
        </button>
        <Link
          to="/clients/new"
          className="app-button-secondary inline-flex rounded-2xl text-sm font-medium"
        >
          Create client
        </Link>
      </div>
    </section>
  );
}

function describeHeadline(summary: ControlRoomResponse): string {
  if (summary.onboarding.needs_first_server) {
    return "Your panel is ready. Connect the first Telemt server to unlock live health, recent activity, and one-click control actions from one calm workspace.";
  }

  if (summary.fleet.total_agents == 1) {
    return "One server is connected and reporting in. Keep an eye on health, recent actions, and current Telemt activity without leaving the room.";
  }

  return "Your servers are reporting into one place. Use Control Room to see what is healthy, what needs attention, and what happened most recently.";
}

function HeroPill(props: { label: string; value: string }) {
  return (
    <div className="app-card-muted rounded-[24px] px-4 py-4 shadow-[0_18px_36px_rgba(37,46,68,0.08)]">
      <p className="text-xs font-semibold uppercase tracking-[0.22em] text-[var(--app-text-tertiary)]">{props.label}</p>
      <p className="mt-3 text-3xl font-semibold tracking-tight text-[var(--app-text-primary)]">{props.value}</p>
    </div>
  );
}
