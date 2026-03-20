import { type ControlRoomResponse } from "../lib/api";
import { AgentInstallFlow } from "./agent-install-flow";

type ControlRoomOnboardingProps = {
  onboarding: ControlRoomResponse["onboarding"];
  open: boolean;
  onOpenChange: (open: boolean) => void;
};

export function ControlRoomOnboarding(props: ControlRoomOnboardingProps) {
  if (!props.open) {
    return null;
  }

  return (
    <section id="dashboard-onboarding" className="rounded-[32px] border border-white/80 bg-white/85 p-6 shadow-[0_20px_60px_rgba(37,46,68,0.08)]">
      <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
        <div className="max-w-2xl">
          <p className="text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">
            {props.onboarding.needs_first_server ? "First connection" : "Add another node"}
          </p>
          <h3 className="mt-2 text-2xl font-semibold tracking-tight text-slate-950">
            {props.onboarding.needs_first_server ? "Bring your first Telemt server into the room" : "Connect another Telemt server"}
          </h3>
          <p className="mt-3 text-sm leading-6 text-slate-600">
            {props.onboarding.needs_first_server
              ? "Create a short-lived enrollment token, run the installer on the Linux server that hosts Telemt, and let Panvex confirm the first full connection for you."
              : "Create a fresh enrollment token, run the installer on the next Linux host, and add another Telemt node into the fleet without leaving the dashboard."}
          </p>
        </div>
        {props.onboarding.setup_complete ? (
          <button
            type="button"
            className="inline-flex rounded-2xl border border-slate-200 bg-white px-4 py-3 text-sm font-medium text-slate-700 transition hover:border-slate-300 hover:text-slate-950"
            onClick={() => props.onOpenChange(false)}
          >
            Hide steps
          </button>
        ) : null}
      </div>

      <div className="mt-6">
        <AgentInstallFlow
          initialFleetGroupID={props.onboarding.suggested_fleet_group_id}
          createLabel={props.onboarding.needs_first_server ? "Create first token" : "Create another token"}
          onFinish={() => props.onOpenChange(false)}
        />
      </div>
    </section>
  );
}
