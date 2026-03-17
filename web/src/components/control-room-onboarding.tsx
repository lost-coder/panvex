import { useEffect, useState } from "react";

import { type ControlRoomResponse } from "../lib/api";
import { AgentInstallFlow } from "./agent-install-flow";

type ControlRoomOnboardingProps = {
  onboarding: ControlRoomResponse["onboarding"];
};

export function ControlRoomOnboarding(props: ControlRoomOnboardingProps) {
  const [expanded, setExpanded] = useState(props.onboarding.needs_first_server);

  useEffect(() => {
    setExpanded(props.onboarding.needs_first_server);
  }, [props.onboarding.needs_first_server]);

  if (!expanded && props.onboarding.setup_complete) {
    return (
      <section className="rounded-[32px] border border-white/80 bg-white/85 p-5 shadow-[0_20px_60px_rgba(37,46,68,0.08)]">
        <div className="flex flex-col gap-4 md:flex-row md:items-center md:justify-between">
          <div>
            <p className="text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">Setup complete</p>
            <h3 className="mt-2 text-2xl font-semibold tracking-tight text-slate-950">Your first server is connected</h3>
            <p className="mt-2 text-sm text-slate-600">
              Keep this card nearby when you want to connect another server or reopen the onboarding steps.
            </p>
          </div>
          <button
            type="button"
            className="inline-flex rounded-2xl border border-slate-200 bg-white px-4 py-3 text-sm font-medium text-slate-700 transition hover:border-slate-300 hover:text-slate-950"
            onClick={() => setExpanded(true)}
          >
            Show connection steps
          </button>
        </div>
      </section>
    );
  }

  return (
    <section className="rounded-[32px] border border-white/80 bg-white/85 p-6 shadow-[0_20px_60px_rgba(37,46,68,0.08)]">
      <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
        <div className="max-w-2xl">
          <p className="text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">First connection</p>
          <h3 className="mt-2 text-2xl font-semibold tracking-tight text-slate-950">Bring your first Telemt server into the room</h3>
          <p className="mt-3 text-sm leading-6 text-slate-600">
            Create a short-lived enrollment token, run the installer on the Linux server that hosts Telemt, and let Panvex confirm the first full connection for you.
          </p>
        </div>
        {props.onboarding.setup_complete ? (
          <button
            type="button"
            className="inline-flex rounded-2xl border border-slate-200 bg-white px-4 py-3 text-sm font-medium text-slate-700 transition hover:border-slate-300 hover:text-slate-950"
            onClick={() => setExpanded(false)}
          >
            Hide steps
          </button>
        ) : null}
      </div>

      <div className="mt-6">
        <AgentInstallFlow
          initialFleetGroupID={props.onboarding.suggested_fleet_group_id}
          createLabel={props.onboarding.needs_first_server ? "Create first token" : "Create another token"}
          onFinish={() => setExpanded(false)}
        />
      </div>
    </section>
  );
}
