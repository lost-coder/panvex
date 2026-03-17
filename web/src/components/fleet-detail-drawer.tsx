import * as Dialog from "@radix-ui/react-dialog";

import type { Agent, Instance } from "../lib/api";

type FleetDetailDrawerProps = {
  agent: Agent | null;
  instances: Instance[];
  open: boolean;
  onOpenChange: (open: boolean) => void;
};

export function FleetDetailDrawer(props: FleetDetailDrawerProps) {
  const scopedInstances = props.agent
    ? props.instances.filter((instance) => instance.agent_id === props.agent?.id)
    : [];

  return (
    <Dialog.Root open={props.open} onOpenChange={props.onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 bg-slate-950/35 backdrop-blur-sm" />
        <Dialog.Content className="fixed right-4 top-4 bottom-4 z-50 w-[min(520px,calc(100vw-2rem))] overflow-y-auto rounded-[32px] border border-white/70 bg-white p-6 shadow-[0_24px_80px_rgba(15,23,42,0.28)]">
          {props.agent ? (
            <>
              <div className="flex items-start justify-between gap-4">
                <div>
                  <Dialog.Title className="text-2xl font-semibold tracking-tight text-slate-950">
                    {props.agent.node_name}
                  </Dialog.Title>
                  <Dialog.Description className="mt-2 text-sm text-slate-600">
                    {props.agent.fleet_group_id || "Ungrouped"}
                  </Dialog.Description>
                  <p className="mt-3 max-w-md text-sm leading-6 text-slate-600">
                    This drawer keeps the important details for the selected server close by, including the Telemt runtimes that were last reported by its local agent.
                  </p>
                </div>
                <Dialog.Close className="rounded-full border border-slate-200 px-3 py-1.5 text-sm text-slate-500 transition hover:border-slate-300 hover:text-slate-900">
                  Close
                </Dialog.Close>
              </div>

              <section className="mt-8 space-y-5">
                <div className="grid grid-cols-2 gap-4">
                  <StatCard label="Server ID" value={props.agent.id} />
                  <StatCard label="Version" value={props.agent.version || "unknown"} />
                  <StatCard label="Read only" value={props.agent.read_only ? "Yes" : "No"} />
                  <StatCard label="Last seen" value={new Date(props.agent.last_seen_at).toLocaleString()} />
                </div>

                <div>
                  <h3 className="text-sm font-semibold uppercase tracking-[0.22em] text-slate-500">Telemt instances</h3>
                  <div className="mt-4 space-y-3">
                    {scopedInstances.length === 0 ? (
                      <div className="rounded-2xl border border-dashed border-slate-300 px-4 py-5 text-sm text-slate-500">
                        This server has not reported any Telemt instances yet.
                      </div>
                    ) : (
                      scopedInstances.map((instance) => (
                        <div key={instance.id} className="rounded-3xl border border-slate-200 bg-slate-50 p-4">
                          <div className="flex items-center justify-between gap-4">
                            <div>
                              <p className="font-medium text-slate-950">{instance.name}</p>
                              <p className="mt-1 text-sm text-slate-600">{instance.version}</p>
                            </div>
                            <span className="rounded-full bg-slate-950 px-3 py-1 text-xs uppercase tracking-[0.24em] text-white">
                              {instance.connected_users} users
                            </span>
                          </div>
                          <p className="mt-3 text-xs uppercase tracking-[0.22em] text-slate-500">
                            Config {instance.config_fingerprint}
                          </p>
                        </div>
                      ))
                    )}
                  </div>
                </div>
              </section>
            </>
          ) : null}
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

function StatCard(props: { label: string; value: string }) {
  return (
    <div className="rounded-3xl border border-slate-200 bg-slate-50 p-4">
      <div className="text-xs font-semibold uppercase tracking-[0.22em] text-slate-500">{props.label}</div>
      <div className="mt-3 text-sm font-medium text-slate-950">{props.value}</div>
    </div>
  );
}
