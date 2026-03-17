import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { type ReactNode, useEffect, useMemo, useState } from "react";

import {
  apiClient,
  type Agent,
  type Client,
  type ClientDeployment,
  type ClientListItem
} from "./lib/api";
import { ClientAssignmentPicker, ClientAssignmentSummary } from "./components/client-assignment-picker";
import {
  clientToForm,
  emptyClientForm,
  formToClientInput,
  summarizeAssignments,
  type ClientFormErrors,
  type ClientFormState,
  validateClientForm
} from "./clients-form-state";

type ClientEditorProps = {
  mode: "create" | "detail";
  agents: Agent[];
  value: ClientFormState;
  onChange: (next: ClientFormState) => void;
  onSubmit: () => void;
  submitLabel: string;
  submitPending: boolean;
  submitDisabled?: boolean;
  client?: Client;
  validation?: ClientFormErrors;
  submitError?: string | null;
  rotatePending?: boolean;
  deletePending?: boolean;
  onRotateSecret?: () => void;
  onDelete?: () => void;
};

export function ClientsPage() {
  const clientsQuery = useQuery({
    queryKey: ["clients"],
    queryFn: () => apiClient.clients()
  });

  if (clientsQuery.isLoading) {
    return <CenteredMessage title="Loading clients" description="Gathering managed Telemt clients and the latest rollout state." />;
  }

  if (clientsQuery.isError) {
    return <CenteredMessage title="Clients are unavailable" description="The control-plane could not load the current client registry." />;
  }

  const clients = clientsQuery.data ?? [];

  return (
    <div className="space-y-6">
      <section className="rounded-[32px] border border-white/70 bg-white/85 p-6 shadow-[0_20px_60px_rgba(37,46,68,0.08)]">
        <div className="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
          <div>
            <p className="text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">Clients</p>
            <h3 className="mt-2 text-2xl font-semibold tracking-tight text-slate-950">Managed Telemt access</h3>
            <p className="mt-3 max-w-2xl text-sm leading-6 text-slate-600">
              Create a client once, keep the secret and limits in one place, then track rollout status and live usage across every assigned node.
            </p>
          </div>
          <Link
            to="/clients/new"
            className="inline-flex items-center justify-center rounded-2xl bg-slate-950 px-5 py-3 text-sm font-medium text-white transition hover:bg-slate-800"
          >
            Create client
          </Link>
        </div>

        {clients.length > 0 ? (
          <div className="mt-6 overflow-x-auto">
            <table className="min-w-full border-separate border-spacing-y-3">
              <thead>
                <tr className="text-left text-xs font-semibold uppercase tracking-[0.22em] text-slate-500">
                  <th className="px-4 pb-1">Client</th>
                  <th className="px-4 pb-1">State</th>
                  <th className="px-4 pb-1">Assigned nodes</th>
                  <th className="px-4 pb-1">Usage</th>
                  <th className="px-4 pb-1">Expiration</th>
                  <th className="px-4 pb-1">Deploy</th>
                </tr>
              </thead>
              <tbody>
                {clients.map((client) => (
                  <tr key={client.id} className="rounded-3xl bg-slate-50 transition hover:bg-slate-100">
                    <td className="rounded-l-3xl px-4 py-4">
                      <Link to="/clients/$clientId" params={{ clientId: client.id }} className="font-medium text-slate-950 hover:underline">
                        {client.name}
                      </Link>
                      <div className="mt-1 text-sm text-slate-500">{client.id}</div>
                    </td>
                    <td className="px-4 py-4">
                      <StatusPill tone={client.enabled ? "success" : "muted"} label={client.enabled ? "Enabled" : "Disabled"} />
                    </td>
                    <td className="px-4 py-4 text-sm text-slate-700">{client.assigned_nodes_count}</td>
                    <td className="px-4 py-4 text-sm text-slate-700">
                      <div>{formatBytes(client.traffic_used_bytes)} / {formatBytes(client.data_quota_bytes)}</div>
                      <div className="mt-1 text-xs text-slate-500">{client.active_tcp_conns} active TCP, {client.unique_ips_used} IPs</div>
                    </td>
                    <td className="px-4 py-4 text-sm text-slate-700">{formatExpiration(client.expiration_rfc3339)}</td>
                    <td className="rounded-r-3xl px-4 py-4">
                      <StatusPill tone={deployTone(client.last_deploy_status)} label={formatDeployStatus(client.last_deploy_status)} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <FriendlyEmptyState
            title="No managed clients yet"
            description="Create the first client to start distributing secrets, limits, and connection links across your Telemt nodes."
          />
        )}
      </section>
    </div>
  );
}

export function CreateClientPage() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const agentsQuery = useQuery({
    queryKey: ["agents"],
    queryFn: () => apiClient.agents()
  });
  const [form, setForm] = useState<ClientFormState>(emptyClientForm());
  const [validation, setValidation] = useState<ClientFormErrors>({});
  const [submitError, setSubmitError] = useState<string | null>(null);

  const createMutation = useMutation({
    mutationFn: () => apiClient.createClient(formToClientInput(form)),
    onSuccess: async (client) => {
      setValidation({});
      setSubmitError(null);
      await queryClient.invalidateQueries({ queryKey: ["clients"] });
      await queryClient.invalidateQueries({ queryKey: ["client"] });
      navigate({ to: "/clients/$clientId", params: { clientId: client.id } });
    },
    onError: (error: Error) => {
      setSubmitError(error.message);
    }
  });

  if (agentsQuery.isLoading) {
    return <CenteredMessage title="Preparing client creation" description="Loading the latest fleet topology for assignment rules." />;
  }

  if (agentsQuery.isError) {
    return <CenteredMessage title="Client creation is unavailable" description="The current fleet inventory could not be loaded." />;
  }

  return (
    <ClientEditor
      mode="create"
      agents={agentsQuery.data ?? []}
      value={form}
      onChange={(next) => {
        setForm(next);
        setSubmitError(null);
        if (Object.keys(validation).length > 0) {
          setValidation(validateClientForm(next));
        }
      }}
      onSubmit={() => {
        const nextValidation = validateClientForm(form);
        setValidation(nextValidation);
        if (Object.keys(nextValidation).length > 0) {
          return;
        }
        setSubmitError(null);
        createMutation.mutate();
      }}
      submitLabel="Create client"
      submitPending={createMutation.isPending}
      submitDisabled={createMutation.isPending}
      validation={validation}
      submitError={submitError}
    />
  );
}

export function ClientDetailPage() {
  const { clientId } = useParams({ from: "/shell/clients/$clientId" });
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const clientQuery = useQuery({
    queryKey: ["client", clientId],
    queryFn: () => apiClient.client(clientId)
  });
  const agentsQuery = useQuery({
    queryKey: ["agents"],
    queryFn: () => apiClient.agents()
  });
  const [form, setForm] = useState<ClientFormState>(emptyClientForm());
  const [validation, setValidation] = useState<ClientFormErrors>({});
  const [submitError, setSubmitError] = useState<string | null>(null);

  useEffect(() => {
    if (clientQuery.data) {
      setForm(clientToForm(clientQuery.data));
      setValidation({});
      setSubmitError(null);
    }
  }, [clientQuery.data]);

  const updateMutation = useMutation({
    mutationFn: () => apiClient.updateClient(clientId, formToClientInput(form)),
    onSuccess: async (client) => {
      setValidation({});
      setSubmitError(null);
      queryClient.setQueryData(["client", clientId], client);
      await queryClient.invalidateQueries({ queryKey: ["clients"] });
    },
    onError: (error: Error) => {
      setSubmitError(error.message);
    }
  });
  const rotateMutation = useMutation({
    mutationFn: () => apiClient.rotateClientSecret(clientId),
    onSuccess: async (client) => {
      queryClient.setQueryData(["client", clientId], client);
      await queryClient.invalidateQueries({ queryKey: ["clients"] });
    }
  });
  const deleteMutation = useMutation({
    mutationFn: () => apiClient.deleteClient(clientId),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["clients"] });
      navigate({ to: "/clients" });
    }
  });

  if (clientQuery.isLoading || agentsQuery.isLoading) {
    return <CenteredMessage title="Loading client" description="Pulling together limits, rollout state, and the latest usage summary." />;
  }

  if (clientQuery.isError || !clientQuery.data || agentsQuery.isError) {
    return <CenteredMessage title="Client is unavailable" description="The requested client could not be loaded from the control-plane." />;
  }

  const client = clientQuery.data;

  return (
    <div className="space-y-6">
      <ClientEditor
      mode="detail"
      agents={agentsQuery.data ?? []}
      value={form}
      onChange={(next) => {
        setForm(next);
        setSubmitError(null);
        if (Object.keys(validation).length > 0) {
          setValidation(validateClientForm(next));
        }
      }}
      onSubmit={() => {
        const nextValidation = validateClientForm(form);
        setValidation(nextValidation);
        if (Object.keys(nextValidation).length > 0) {
          return;
        }
        setSubmitError(null);
        updateMutation.mutate();
      }}
      submitLabel="Save changes"
      submitPending={updateMutation.isPending}
      client={client}
      validation={validation}
      submitError={submitError}
      rotatePending={rotateMutation.isPending}
      deletePending={deleteMutation.isPending}
        onRotateSecret={() => {
          if (window.confirm("Rotate this client secret and redeploy it to assigned nodes?")) {
            rotateMutation.mutate();
          }
        }}
        onDelete={() => {
          if (window.confirm("Delete this client and remove it from assigned nodes?")) {
            deleteMutation.mutate();
          }
        }}
      />
    </div>
  );
}

function ClientEditor(props: ClientEditorProps) {
  const groupOptions = useMemo(() => {
    const seen = new Map<string, { id: string; label: string }>();
    for (const agent of props.agents) {
      if (agent.fleet_group_id === "" || seen.has(agent.fleet_group_id)) {
        continue;
      }

      seen.set(agent.fleet_group_id, {
        id: agent.fleet_group_id,
        label: agent.fleet_group_id
      });
    }
    return [...seen.values()].sort((left, right) => left.label.localeCompare(right.label));
  }, [props.agents]);

  const assignmentSummary = useMemo(
    () => summarizeAssignments(props.value, props.agents),
    [props.agents, props.value]
  );

  return (
    <div className="space-y-6">
      <section className="rounded-[32px] border border-white/70 bg-white/85 p-6 shadow-[0_20px_60px_rgba(37,46,68,0.08)]">
        <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
          <div>
            <p className="text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">Identity</p>
            <h3 className="mt-2 text-2xl font-semibold tracking-tight text-slate-950">
              {props.mode === "create" ? "Create a managed client" : props.client?.name ?? "Client details"}
            </h3>
            <p className="mt-3 max-w-2xl text-sm leading-6 text-slate-600">
              Keep the secret, ad tag, limits, and rollout rules together so the same client definition can be pushed to every selected node.
            </p>
          </div>
          <div className="flex flex-wrap gap-3">
            {props.mode === "detail" && props.onRotateSecret ? (
              <button
                type="button"
                className="rounded-2xl border border-slate-200 bg-slate-50 px-4 py-3 text-sm font-medium text-slate-700 transition hover:bg-slate-100 disabled:opacity-60"
                onClick={props.onRotateSecret}
                disabled={props.rotatePending}
              >
                {props.rotatePending ? "Rotating..." : "Rotate secret"}
              </button>
            ) : null}
            {props.mode === "detail" && props.onDelete ? (
              <button
                type="button"
                className="rounded-2xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm font-medium text-rose-700 transition hover:bg-rose-100 disabled:opacity-60"
                onClick={props.onDelete}
                disabled={props.deletePending}
              >
                {props.deletePending ? "Deleting..." : "Delete client"}
              </button>
            ) : null}
            <button
              type="button"
              className="rounded-2xl bg-slate-950 px-5 py-3 text-sm font-medium text-white transition hover:bg-slate-800 disabled:opacity-60"
              onClick={props.onSubmit}
              disabled={props.submitDisabled}
            >
              {props.submitPending ? "Saving..." : props.submitLabel}
            </button>
          </div>
        </div>

        {props.submitError ? (
          <div className="mt-6 rounded-3xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700">
            {props.submitError}
          </div>
        ) : null}

        <div className="mt-6 grid gap-6 xl:grid-cols-[1.1fr,0.9fr]">
          <SectionCard title="Identity" description="The central name and secret that Panvex keeps for this client.">
            <div className={`grid gap-4 ${props.mode === "detail" ? "md:grid-cols-2" : ""}`}>
              <div>
                <Field label="Client name" value={props.value.name} onChange={(value) => props.onChange({ ...props.value, name: value })} />
                {props.validation?.name ? <p className="mt-2 text-sm text-rose-700">{props.validation.name}</p> : null}
              </div>
              {props.mode === "detail" ? (
                <label className="block">
                  <span className="mb-2 block text-sm font-medium text-slate-700">State</span>
                  <button
                    type="button"
                    className={`inline-flex rounded-2xl px-4 py-3 text-sm font-medium transition ${props.value.enabled ? "bg-emerald-100 text-emerald-800 hover:bg-emerald-200" : "bg-slate-200 text-slate-700 hover:bg-slate-300"}`}
                    onClick={() => props.onChange({ ...props.value, enabled: !props.value.enabled })}
                  >
                    {props.value.enabled ? "Enabled" : "Disabled"}
                  </button>
                </label>
              ) : null}
            </div>
            <div className="mt-4 grid gap-4 md:grid-cols-2">
              {props.mode === "create" ? (
                <div className="space-y-4">
                  <div>
                    <span className="mb-2 block text-sm font-medium text-slate-700">Ad tag</span>
                    <div className="flex flex-wrap gap-2">
                      <button
                        type="button"
                        className={`rounded-2xl px-4 py-2 text-sm font-medium transition ${props.value.adTagMode === "auto" ? "bg-slate-950 text-white" : "border border-slate-200 bg-slate-50 text-slate-700 hover:bg-slate-100"}`}
                        onClick={() => props.onChange({ ...props.value, adTagMode: "auto" })}
                      >
                        Generate automatically
                      </button>
                      <button
                        type="button"
                        className={`rounded-2xl px-4 py-2 text-sm font-medium transition ${props.value.adTagMode === "manual" ? "bg-slate-950 text-white" : "border border-slate-200 bg-slate-50 text-slate-700 hover:bg-slate-100"}`}
                        onClick={() => props.onChange({ ...props.value, adTagMode: "manual" })}
                      >
                        Enter manually
                      </button>
                    </div>
                  </div>
                  {props.value.adTagMode === "manual" ? (
                    <div>
                      <Field label="Manual ad tag" value={props.value.userADTag} onChange={(value) => props.onChange({ ...props.value, userADTag: value })} />
                      {props.validation?.userADTag ? <p className="mt-2 text-sm text-rose-700">{props.validation.userADTag}</p> : null}
                    </div>
                  ) : (
                    <div className="rounded-3xl border border-dashed border-slate-300 bg-slate-50/80 p-4 text-sm text-slate-600">
                      Panvex will generate a 32-character ad tag automatically when the client is created.
                    </div>
                  )}
                </div>
              ) : (
                <div>
                  <Field label="Ad tag" value={props.value.userADTag} onChange={(value) => props.onChange({ ...props.value, userADTag: value })} />
                  {props.validation?.userADTag ? <p className="mt-2 text-sm text-rose-700">{props.validation.userADTag}</p> : null}
                </div>
              )}
              {props.client ? (
                <div className="rounded-3xl border border-slate-200 bg-slate-50 p-4">
                  <p className="text-sm font-medium text-slate-900">Current secret</p>
                  <p className="mt-2 break-all font-mono text-xs text-slate-600">{props.client.secret}</p>
                </div>
              ) : (
                <div className="rounded-3xl border border-dashed border-slate-300 bg-slate-50/80 p-4 text-sm text-slate-600">
                  Panvex will generate the secret automatically and show it on the detail page right after creation.
                </div>
              )}
            </div>
          </SectionCard>

          {props.client ? (
            <SectionCard title="Usage" description="Current aggregated usage across assigned nodes.">
              <div className="grid gap-4 sm:grid-cols-3">
                <MetricCard label="Traffic used" value={formatBytes(props.client.traffic_used_bytes)} />
                <MetricCard label="Unique IPs" value={String(props.client.unique_ips_used)} />
                <MetricCard label="Active TCP" value={String(props.client.active_tcp_conns)} />
              </div>
            </SectionCard>
          ) : null}
        </div>
      </section>

      <div className="grid gap-6 xl:grid-cols-[0.9fr,1.1fr]">
        <SectionCard title="Limits" description="Telemt-side limits and expiration applied to this client.">
          <div className="grid gap-4 md:grid-cols-2">
            <Field label="Max TCP connections" type="number" value={props.value.maxTCPConns} onChange={(value) => props.onChange({ ...props.value, maxTCPConns: value })} />
            <Field label="Max unique IPs" type="number" value={props.value.maxUniqueIPs} onChange={(value) => props.onChange({ ...props.value, maxUniqueIPs: value })} />
            <Field label="Data quota in bytes" type="number" value={props.value.dataQuotaBytes} onChange={(value) => props.onChange({ ...props.value, dataQuotaBytes: value })} />
            <Field label="Expiration (RFC3339)" value={props.value.expirationRFC3339} onChange={(value) => props.onChange({ ...props.value, expirationRFC3339: value })} placeholder="2026-04-01T00:00:00Z" />
          </div>
        </SectionCard>

        <SectionCard title="Assignments" description="Choose groups and explicit nodes. Panvex applies the union of every selected rule.">
          {props.validation?.assignments ? (
            <div className="mb-4 rounded-3xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700">
              {props.validation.assignments}
            </div>
          ) : null}
          <div className="grid gap-5 lg:grid-cols-2">
            <ClientAssignmentPicker
              title="Fleet groups"
              description="Apply to every node inside a specific fleet group."
              searchPlaceholder="Search groups"
              options={groupOptions}
              selected={props.value.fleetGroupIDs}
              onToggle={(fleetGroupID) => props.onChange({ ...props.value, fleetGroupIDs: toggleSelection(props.value.fleetGroupIDs, fleetGroupID) })}
            />
            <ClientAssignmentPicker
              title="Explicit nodes"
              description="Add individual nodes on top of group rules."
              searchPlaceholder="Search nodes"
              options={props.agents.map((agent) => ({ id: agent.id, label: formatAgentOptionLabel(agent) }))}
              selected={props.value.agentIDs}
              onToggle={(agentID) => props.onChange({ ...props.value, agentIDs: toggleSelection(props.value.agentIDs, agentID) })}
            />
          </div>
          <ClientAssignmentSummary summary={assignmentSummary} />
        </SectionCard>
      </div>

      {props.client ? (
        <SectionCard title="Deployment" description="Per-node rollout status and the latest connection links returned by Telemt.">
          {props.client.deployments.length > 0 ? (
            <div className="space-y-3">
              {props.client.deployments.map((deployment) => (
                <div key={deployment.agent_id} className="rounded-3xl border border-slate-200 bg-slate-50 p-4">
                  <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
                    <div>
                      <div className="flex flex-wrap items-center gap-3">
                        <p className="font-medium text-slate-950">{deployment.agent_id}</p>
                        <StatusPill tone={deployTone(deployment.status)} label={formatDeployStatus(deployment.status)} />
                      </div>
                      <p className="mt-2 text-sm text-slate-600">Desired operation: {formatDeployStatus(deployment.desired_operation)}</p>
                      {deployment.last_error ? <p className="mt-2 text-sm text-rose-700">{deployment.last_error}</p> : null}
                    </div>
                    <div className="max-w-xl flex-1 rounded-2xl border border-slate-200 bg-white px-4 py-3">
                      <div className="flex items-start justify-between gap-3">
                        <div>
                          <p className="text-xs font-semibold uppercase tracking-[0.2em] text-slate-500">Connection link</p>
                          <p className="mt-2 break-all text-sm text-slate-700">
                            {deployment.connection_link || "Waiting for Telemt to return the latest link."}
                          </p>
                          <p className="mt-2 text-xs text-slate-500">
                            Last applied: {deployment.last_applied_at_unix > 0 ? formatUnix(deployment.last_applied_at_unix) : "Not applied yet"}
                          </p>
                        </div>
                        {deployment.connection_link ? <CopyButton text={deployment.connection_link} /> : null}
                      </div>
                    </div>
                  </div>
                </div>
              ))}
            </div>
          ) : (
            <FriendlyEmptyState title="No rollout targets yet" description="Save assignments to queue the first rollout job for this client." />
          )}
        </SectionCard>
      ) : null}
    </div>
  );
}

function SectionCard(props: { title: string; description: string; children: ReactNode }) {
  return (
    <section className="rounded-[32px] border border-white/70 bg-white/85 p-6 shadow-[0_20px_60px_rgba(37,46,68,0.08)]">
      <p className="text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">{props.title}</p>
      <p className="mt-2 max-w-2xl text-sm leading-6 text-slate-600">{props.description}</p>
      <div className="mt-6">{props.children}</div>
    </section>
  );
}

function Field(props: { label: string; value: string; onChange: (value: string) => void; type?: string; placeholder?: string }) {
  return (
    <label className="block">
      <span className="mb-2 block text-sm font-medium text-slate-700">{props.label}</span>
      <input
        type={props.type ?? "text"}
        className="w-full rounded-2xl border border-slate-200 bg-slate-50 px-4 py-3 text-sm text-slate-900"
        value={props.value}
        placeholder={props.placeholder}
        onChange={(event) => props.onChange(event.target.value)}
      />
    </label>
  );
}

function MetricCard(props: { label: string; value: string }) {
  return (
    <div className="rounded-3xl border border-slate-200 bg-slate-50 px-4 py-5">
      <p className="text-xs font-semibold uppercase tracking-[0.2em] text-slate-500">{props.label}</p>
      <p className="mt-3 text-2xl font-semibold tracking-tight text-slate-950">{props.value}</p>
    </div>
  );
}

function CopyButton(props: { text: string }) {
  return (
    <button
      type="button"
      className="rounded-2xl border border-slate-200 bg-slate-50 px-3 py-2 text-xs font-medium text-slate-700 transition hover:bg-slate-100"
      onClick={() => void navigator.clipboard.writeText(props.text)}
    >
      Copy
    </button>
  );
}

function StatusPill(props: { tone: "success" | "warning" | "danger" | "muted"; label: string }) {
  const toneClass =
    props.tone === "success"
      ? "bg-emerald-100 text-emerald-800"
      : props.tone === "warning"
        ? "bg-amber-100 text-amber-800"
        : props.tone === "danger"
          ? "bg-rose-100 text-rose-800"
          : "bg-slate-200 text-slate-700";

  return <span className={`rounded-full px-3 py-1 text-xs uppercase tracking-[0.22em] ${toneClass}`}>{props.label}</span>;
}

function FriendlyEmptyState(props: { title: string; description: string }) {
  return (
    <div className="rounded-[28px] border border-dashed border-slate-300 bg-slate-50/80 px-5 py-10 text-center">
      <h4 className="text-lg font-semibold text-slate-950">{props.title}</h4>
      <p className="mt-3 text-sm leading-6 text-slate-600">{props.description}</p>
    </div>
  );
}

function toggleSelection(selected: string[], value: string) {
  return selected.includes(value) ? selected.filter((item) => item !== value) : [...selected, value];
}

function formatAgentOptionLabel(agent: Agent) {
  return `${agent.node_name} (${agent.fleet_group_id || "Ungrouped"})`;
}

function formatBytes(value: number) {
  if (value <= 0) {
    return "0 B";
  }

  const units = ["B", "KB", "MB", "GB", "TB"];
  let amount = value;
  let unitIndex = 0;
  while (amount >= 1024 && unitIndex < units.length - 1) {
    amount /= 1024;
    unitIndex++
  }
  return `${amount.toFixed(amount >= 10 || unitIndex === 0 ? 0 : 1)} ${units[unitIndex]}`
}

function formatExpiration(value: string) {
  if (!value) {
    return "No expiration";
  }
  return new Date(value).toLocaleString();
}

function formatUnix(value: number) {
  return new Date(value * 1000).toLocaleString();
}

function formatDeployStatus(value: string) {
  return value.replaceAll("_", " ");
}

function deployTone(status: string): "success" | "warning" | "danger" | "muted" {
  switch (status) {
    case "succeeded":
    case "enabled":
      return "success";
    case "pending":
    case "queued":
      return "warning";
    case "failed":
      return "danger";
    default:
      return "muted";
  }
}

function CenteredMessage(props: { title: string; description: string }) {
  return (
    <div className="flex min-h-[50vh] items-center justify-center">
      <div className="max-w-lg rounded-[32px] border border-white/70 bg-white/85 p-8 text-center shadow-[0_20px_60px_rgba(37,46,68,0.08)]">
        <h3 className="text-2xl font-semibold tracking-tight text-slate-950">{props.title}</h3>
        <p className="mt-3 text-sm text-slate-600">{props.description}</p>
      </div>
    </div>
  );
}
