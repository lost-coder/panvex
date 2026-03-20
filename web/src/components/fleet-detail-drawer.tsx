import * as Dialog from "@radix-ui/react-dialog";
import type { ReactNode } from "react";

import type { Agent, Instance } from "../lib/api";
import { FleetRuntimeModeBadge, FleetRuntimeStatusBadge } from "./fleet-runtime-status-badge";
import { FleetRuntimeConnections, FleetRuntimeDCSummary, FleetRuntimeUpstreamSummary } from "./fleet-runtime-summary";

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
        <Dialog.Overlay className="app-overlay fixed inset-0 backdrop-blur-sm" />
        <Dialog.Content className="app-panel-strong fixed right-4 top-4 bottom-4 z-50 w-[min(520px,calc(100vw-2rem))] overflow-y-auto rounded-[32px] p-6">
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
                <div className="flex flex-wrap gap-3">
                  <FleetRuntimeStatusBadge agent={props.agent} />
                  <FleetRuntimeModeBadge agent={props.agent} />
                </div>

                <div className="grid grid-cols-2 gap-4">
                  <StatCard label="Server ID" value={props.agent.id} />
                  <StatCard label="Version" value={props.agent.version || "unknown"} />
                  <StatCard label="Read only" value={props.agent.read_only ? "Yes" : "No"} />
                  <StatCard label="Last seen" value={new Date(props.agent.last_seen_at).toLocaleString()} />
                </div>

                <div className="grid gap-4 md:grid-cols-3">
                  <StatBlock label="Connections">
                    <FleetRuntimeConnections agent={props.agent} />
                  </StatBlock>
                  <StatBlock label="DC coverage">
                    <FleetRuntimeDCSummary agent={props.agent} />
                  </StatBlock>
                  <StatBlock label="Upstreams">
                    <FleetRuntimeUpstreamSummary agent={props.agent} />
                  </StatBlock>
                </div>

                <div>
                  <h3 className="text-sm font-semibold uppercase tracking-[0.22em] text-slate-500">Recent runtime events</h3>
                  <div className="mt-4 space-y-3">
                    {props.agent.runtime.recent_events.length > 0 ? (
                      props.agent.runtime.recent_events.map((event) => (
                        <div key={`${event.sequence}-${event.timestamp_unix}`} className="rounded-3xl border border-slate-200 bg-slate-50 p-4">
                          <div className="flex items-center justify-between gap-4">
                            <p className="font-medium text-slate-950">{event.event_type.replaceAll("_", " ")}</p>
                            <span className="text-xs uppercase tracking-[0.22em] text-slate-500">
                              {new Date(event.timestamp_unix * 1000).toLocaleString()}
                            </span>
                          </div>
                          <p className="mt-2 text-sm text-slate-600">{event.context}</p>
                        </div>
                      ))
                    ) : (
                      <div className="rounded-2xl border border-dashed border-slate-300 px-4 py-5 text-sm text-slate-500">
                        This server has not reported any recent Telemt runtime events yet.
                      </div>
                    )}
                  </div>
                </div>

                <div>
                  <h3 className="text-sm font-semibold uppercase tracking-[0.22em] text-slate-500">DC health</h3>
                  <div className="mt-4 space-y-3">
                    {props.agent.runtime.dcs.length > 0 ? (
                      props.agent.runtime.dcs.map((dc) => (
                        <div key={dc.dc} className="rounded-3xl border border-slate-200 bg-slate-50 p-4">
                          <div className="flex items-center justify-between gap-4">
                            <div>
                              <p className="font-medium text-slate-950">DC {dc.dc}</p>
                              <p className="mt-1 text-sm text-slate-600">
                                {dc.available_endpoints} endpoints, {dc.alive_writers}/{dc.required_writers} writers alive
                              </p>
                            </div>
                            <span className="rounded-full bg-slate-950 px-3 py-1 text-xs uppercase tracking-[0.24em] text-white">
                              {Math.round(dc.coverage_pct)}% coverage
                            </span>
                          </div>
                          <div className="mt-4 grid gap-3 text-sm text-slate-600 sm:grid-cols-3">
                            <div>
                              <p className="text-xs font-semibold uppercase tracking-[0.22em] text-slate-500">Availability</p>
                              <p className="mt-2 font-medium text-slate-950">{Math.round(dc.available_pct)}%</p>
                            </div>
                            <div>
                              <p className="text-xs font-semibold uppercase tracking-[0.22em] text-slate-500">RTT</p>
                              <p className="mt-2 font-medium text-slate-950">{dc.rtt_ms.toFixed(1)} ms</p>
                            </div>
                            <div>
                              <p className="text-xs font-semibold uppercase tracking-[0.22em] text-slate-500">Load</p>
                              <p className="mt-2 font-medium text-slate-950">{dc.load}</p>
                            </div>
                          </div>
                        </div>
                      ))
                    ) : (
                      <div className="rounded-2xl border border-dashed border-slate-300 px-4 py-5 text-sm text-slate-500">
                        This server has not reported any DC health rows yet.
                      </div>
                    )}
                  </div>
                </div>

                <div>
                  <h3 className="text-sm font-semibold uppercase tracking-[0.22em] text-slate-500">Upstreams</h3>
                  <div className="mt-4 space-y-3">
                    {props.agent.runtime.upstreams.length > 0 ? (
                      props.agent.runtime.upstreams.map((upstream) => (
                        <div key={`${upstream.upstream_id}-${upstream.address}`} className="rounded-3xl border border-slate-200 bg-slate-50 p-4">
                          <div className="flex items-center justify-between gap-4">
                            <div>
                              <p className="font-medium text-slate-950">{upstream.route_kind}</p>
                              <p className="mt-1 text-sm text-slate-600">{upstream.address}</p>
                            </div>
                            <span className={`rounded-full px-3 py-1 text-xs uppercase tracking-[0.24em] ${upstream.healthy ? "bg-emerald-100 text-emerald-800" : "bg-amber-100 text-amber-800"}`}>
                              {upstream.healthy ? "Healthy" : "Degraded"}
                            </span>
                          </div>
                          <div className="mt-4 grid gap-3 text-sm text-slate-600 sm:grid-cols-2">
                            <div>
                              <p className="text-xs font-semibold uppercase tracking-[0.22em] text-slate-500">Latency</p>
                              <p className="mt-2 font-medium text-slate-950">{upstream.effective_latency_ms.toFixed(1)} ms</p>
                            </div>
                            <div>
                              <p className="text-xs font-semibold uppercase tracking-[0.22em] text-slate-500">Fails</p>
                              <p className="mt-2 font-medium text-slate-950">{upstream.fails}</p>
                            </div>
                          </div>
                        </div>
                      ))
                    ) : (
                      <div className="rounded-2xl border border-dashed border-slate-300 px-4 py-5 text-sm text-slate-500">
                        This server has not reported any upstream health rows yet.
                      </div>
                    )}
                  </div>
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

function StatBlock(props: { label: string; children: ReactNode }) {
  return (
    <div className="rounded-3xl border border-slate-200 bg-slate-50 p-4">
      <div className="text-xs font-semibold uppercase tracking-[0.22em] text-slate-500">{props.label}</div>
      <div className="mt-3">{props.children}</div>
    </div>
  );
}
