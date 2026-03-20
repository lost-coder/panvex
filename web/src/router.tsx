import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Link,
  Navigate,
  Outlet,
  createRootRouteWithContext,
  createRoute,
  createRouter
} from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { Area, AreaChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";

import { AppShell } from "./components/app-shell";
import { ClientDetailPage, ClientsPage, CreateClientPage } from "./clients-page";
import { ControlRoomHero } from "./components/control-room-hero";
import { ControlRoomOnboarding } from "./components/control-room-onboarding";
import { ControlRoomStatusStrip } from "./components/control-room-status-strip";
import { FleetDetailDrawer } from "./components/fleet-detail-drawer";
import { FleetNodeCardGrid } from "./components/fleet-node-card-grid";
import { FleetRuntimeModeBadge, FleetRuntimeStatusBadge } from "./components/fleet-runtime-status-badge";
import { FleetRuntimeConnections, FleetRuntimeDCSummary, FleetRuntimeUpstreamSummary } from "./components/fleet-runtime-summary";
import { FleetNodePage } from "./fleet-node-page";
import { TelemtAttentionPanel } from "./components/telemt-attention-panel";
import { SettingsPage } from "./settings-page";
import {
  apiClient,
  configuredRootPath,
  type Agent,
  type AuditEvent,
  type Job,
  type MetricSnapshot
} from "./lib/api";
import { getRouterBasepath } from "./lib/runtime-path";

type RouterContext = {
  queryClient: import("@tanstack/react-query").QueryClient;
};

const rootRoute = createRootRouteWithContext<RouterContext>()({
  component: () => <Outlet />
});

const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/login",
  component: LoginPage
});

const shellRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: "shell",
  component: ProtectedShell
});

const overviewRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/",
  component: OverviewPage
});

const fleetRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/fleet",
  component: FleetPage
});

const fleetNodeRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/fleet/$agentId",
  component: FleetNodePage
});

const jobsRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/jobs",
  component: JobsPage
});

const auditRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/audit",
  component: AuditPage
});

const agentsRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/agents",
  component: AgentsPage
});

const clientsRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/clients",
  component: ClientsPage
});

const clientsNewRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/clients/new",
  component: CreateClientPage
});

const clientDetailRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/clients/$clientId",
  component: ClientDetailPage
});

const settingsRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/settings",
  component: SettingsPage
});

const routeTree = rootRoute.addChildren([
  loginRoute,
  shellRoute.addChildren([overviewRoute, fleetRoute, fleetNodeRoute, jobsRoute, auditRoute, agentsRoute, clientsRoute, clientsNewRoute, clientDetailRoute, settingsRoute])
]);

export const router = createRouter({
  routeTree,
  basepath: getRouterBasepath(configuredRootPath),
  context: {
    queryClient: undefined as never
  }
});

function ProtectedShell() {
  const meQuery = useQuery({
    queryKey: ["me"],
    queryFn: () => apiClient.me()
  });

  if (meQuery.isLoading) {
    return <CenteredMessage title="Loading Control Room" description="Opening your latest control-plane context." />;
  }

  if (meQuery.isError) {
    return <Navigate to="/login" />;
  }

  return <AppShell />;
}

function LoginPage() {
  const queryClient = useQueryClient();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [totpCode, setTotpCode] = useState("");
  const [error, setError] = useState<string | null>(null);

  const loginMutation = useMutation({
    mutationFn: () =>
      apiClient.login({
        username,
        password,
        totp_code: totpCode || undefined
      }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["me"] });
      router.navigate({ to: "/" });
    },
    onError: (mutationError: Error) => setError(mutationError.message)
  });

  return (
    <div className="flex min-h-screen items-center justify-center bg-[radial-gradient(circle_at_top_left,_rgba(40,127,171,0.22),_transparent_22%),radial-gradient(circle_at_bottom_right,_rgba(3,102,85,0.14),_transparent_30%),linear-gradient(180deg,#f5f1e7_0%,#f7fafc_100%)] px-4 py-12">
      <div className="w-full max-w-xl rounded-[36px] border border-white/70 bg-white/90 p-8 shadow-[0_28px_80px_rgba(15,23,42,0.16)] backdrop-blur">
        <p className="text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">Panvex</p>
        <h1 className="mt-4 text-4xl font-semibold tracking-tight text-slate-950">Enter the control room</h1>
        <p className="mt-3 text-sm leading-6 text-slate-600">
          Sign in with your local account to keep server health, Telemt actions, and day-to-day control in one friendly place.
        </p>
        <form
          className="mt-8 space-y-4"
          onSubmit={(event) => {
            event.preventDefault();
            setError(null);
            loginMutation.mutate();
          }}
        >
          <Field label="Username" value={username} onChange={setUsername} />
          <Field label="Password" type="password" value={password} onChange={setPassword} />
          <Field label="TOTP code" value={totpCode} onChange={setTotpCode} placeholder="Only needed if two-factor authentication is enabled" />
          {error ? <p className="rounded-2xl bg-rose-50 px-4 py-3 text-sm text-rose-700">{error}</p> : null}
          <button
            type="submit"
            className="inline-flex rounded-2xl bg-slate-950 px-5 py-3 text-sm font-medium text-white transition hover:bg-slate-800 disabled:opacity-60"
            disabled={loginMutation.isPending}
          >
            {loginMutation.isPending ? "Authenticating..." : "Enter Control Room"}
          </button>
        </form>
      </div>
    </div>
  );
}

function OverviewPage() {
  const controlRoomQuery = useQuery({ queryKey: ["control-room"], queryFn: () => apiClient.controlRoom() });
  const metricsQuery = useQuery({ queryKey: ["metrics"], queryFn: () => apiClient.metrics() });
  const agentsQuery = useQuery({ queryKey: ["agents"], queryFn: () => apiClient.agents() });
  const [onboardingOpen, setOnboardingOpen] = useState(false);
  const needsFirstServer = controlRoomQuery.data?.onboarding.needs_first_server ?? false;

  useEffect(() => {
    setOnboardingOpen(needsFirstServer);
  }, [needsFirstServer]);

  if (controlRoomQuery.isLoading || agentsQuery.isLoading) {
    return <CenteredMessage title="Loading Control Room" description="Pulling together your latest server summary." />;
  }

  if (controlRoomQuery.isError || agentsQuery.isError) {
    return <CenteredMessage title="Control Room is unavailable" description="The overview could not load the latest control-plane summary." />;
  }

  const controlRoom = controlRoomQuery.data!;
  const agents = agentsQuery.data ?? [];
  const chartData = aggregateMetrics(metricsQuery.data ?? []);

  const handleOpenOnboarding = () => {
    setOnboardingOpen(true);
    window.requestAnimationFrame(() => {
      document.getElementById("dashboard-onboarding")?.scrollIntoView({ behavior: "smooth", block: "start" });
    });
  };

  return (
    <div className="space-y-6">
      <ControlRoomHero summary={controlRoom} onAddNode={handleOpenOnboarding} />
      <ControlRoomOnboarding onboarding={controlRoom.onboarding} open={onboardingOpen || controlRoom.onboarding.needs_first_server} onOpenChange={setOnboardingOpen} />
      <ControlRoomStatusStrip summary={controlRoom} />

      <TelemtAttentionPanel agents={agents} />
      <FleetNodeCardGrid agents={agents} />

      <div className="grid gap-6 xl:grid-cols-[1.2fr,0.8fr]">
        <section className="rounded-[32px] border border-white/70 bg-white/85 p-6 shadow-[0_20px_60px_rgba(37,46,68,0.08)]">
          <p className="text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">Activity</p>
          <h3 className="mt-2 text-2xl font-semibold tracking-tight text-slate-950">Connected users over time</h3>
          {chartData.length > 0 ? (
            <div className="mt-6 h-72">
              <ResponsiveContainer width="100%" height="100%">
                <AreaChart data={chartData}>
                  <defs>
                    <linearGradient id="connectedUsers" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="5%" stopColor="#0f766e" stopOpacity={0.45} />
                      <stop offset="95%" stopColor="#0f766e" stopOpacity={0} />
                    </linearGradient>
                  </defs>
                  <CartesianGrid strokeDasharray="4 6" stroke="#d8dee8" vertical={false} />
                  <XAxis dataKey="label" stroke="#64748b" />
                  <YAxis stroke="#64748b" />
                  <Tooltip />
                  <Area type="monotone" dataKey="connectedUsers" stroke="#0f766e" fill="url(#connectedUsers)" strokeWidth={3} />
                </AreaChart>
              </ResponsiveContainer>
            </div>
          ) : (
            <FriendlyEmptyState
              title="No traffic history yet"
              description="Once your agent starts sending snapshots, the recent connected-user trend will show up here."
            />
          )}
        </section>

        <section className="rounded-[32px] border border-white/70 bg-white/85 p-6 shadow-[0_20px_60px_rgba(37,46,68,0.08)]">
          <p className="text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">Recent runtime events</p>
          <h3 className="mt-2 text-2xl font-semibold tracking-tight text-slate-950">What changed inside Telemt most recently</h3>
          <div className="mt-6 space-y-3">
            {controlRoom.recent_runtime_events.length > 0 ? (
              controlRoom.recent_runtime_events.map((event) => (
                <div key={`${event.sequence}-${event.timestamp_unix}`} className="rounded-3xl border border-slate-200 bg-slate-50 p-4">
                  <div className="flex items-center justify-between gap-4">
                    <div>
                      <p className="font-medium text-slate-950">{event.event_type.replaceAll("_", " ")}</p>
                      <p className="mt-1 text-sm text-slate-600">{event.context}</p>
                    </div>
                    <span className="text-xs uppercase tracking-[0.22em] text-slate-500">
                      {new Date(event.timestamp_unix * 1000).toLocaleString()}
                    </span>
                  </div>
                </div>
              ))
            ) : (
              <FriendlyEmptyState
                title="No runtime events yet"
                description="Telemt runtime events will appear here once nodes start reporting route changes, recovery, or coverage drops."
              />
            )}
          </div>
        </section>
      </div>
    </div>
  );
}

function FleetPage() {
  const agentsQuery = useQuery({ queryKey: ["agents"], queryFn: () => apiClient.agents() });
  const instancesQuery = useQuery({ queryKey: ["instances"], queryFn: () => apiClient.instances() });
  const [selectedAgent, setSelectedAgent] = useState<Agent | null>(null);

  if (agentsQuery.isLoading || instancesQuery.isLoading) {
    return <CenteredMessage title="Loading fleet" description="Gathering the latest server and Telemt inventory." />;
  }

  if (agentsQuery.isError || instancesQuery.isError) {
    return <CenteredMessage title="Fleet is unavailable" description="The current server inventory could not be loaded." />;
  }

  const agents = agentsQuery.data ?? [];
  const instances = instancesQuery.data ?? [];
  const singleServerView = agents.length <= 1;

  if (agents.length === 0) {
    return (
      <FriendlyEmptyState
        title="No servers are connected yet"
        description="Open Control Room to create the first connection token and bring your first Telemt server online."
      />
    );
  }

  return (
    <>
      <div className="rounded-[32px] border border-white/70 bg-white/85 p-6 shadow-[0_20px_60px_rgba(37,46,68,0.08)]">
        <div className="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
          <div>
            <p className="text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">{singleServerView ? "Your server" : "Fleet"}</p>
            <h3 className="mt-2 text-2xl font-semibold tracking-tight text-slate-950">
              {singleServerView ? "Your server and its Telemt runtimes" : "Servers and their Telemt runtimes"}
            </h3>
          </div>
          <p className="max-w-xl text-sm text-slate-600">
            {singleServerView
              ? "This page keeps the important details for your connected server close by. Select the row to inspect the local Telemt runtimes without leaving the table."
              : "Keep your server inventory close by, then open any row to inspect the Telemt runtimes reported by that host."}
          </p>
        </div>
        <div className="mt-6 overflow-x-auto">
          <table className="min-w-full border-separate border-spacing-y-3">
            <thead>
              <tr className="text-left text-xs font-semibold uppercase tracking-[0.22em] text-slate-500">
                <th className="px-4 pb-1">Node</th>
                <th className="px-4 pb-1">Group</th>
                <th className="px-4 pb-1">Status</th>
                <th className="px-4 pb-1">Mode</th>
                <th className="px-4 pb-1">Connections</th>
                <th className="px-4 pb-1">DC</th>
                <th className="px-4 pb-1">Upstreams</th>
                <th className="px-4 pb-1">Last report</th>
              </tr>
            </thead>
            <tbody>
              {agents.map((agent) => (
                <tr
                  key={agent.id}
                  className="cursor-pointer rounded-3xl bg-slate-50 transition hover:bg-slate-100"
                  onClick={() => setSelectedAgent(agent)}
                >
                  <td className="rounded-l-3xl px-4 py-4">
                    <Link
                      to="/fleet/$agentId"
                      params={{ agentId: agent.id }}
                      className="font-medium text-slate-950 hover:underline"
                      onClick={(event) => event.stopPropagation()}
                    >
                      {agent.node_name}
                    </Link>
                    <div className="mt-1 text-sm text-slate-500">{agent.id}</div>
                  </td>
                  <td className="px-4 py-4 text-sm text-slate-700">{agent.fleet_group_id || "Ungrouped"}</td>
                  <td className="px-4 py-4">
                    <FleetRuntimeStatusBadge agent={agent} />
                  </td>
                  <td className="px-4 py-4">
                    <FleetRuntimeModeBadge agent={agent} />
                  </td>
                  <td className="px-4 py-4 text-sm text-slate-700">
                    <FleetRuntimeConnections agent={agent} />
                  </td>
                  <td className="px-4 py-4 text-sm text-slate-700">
                    <FleetRuntimeDCSummary agent={agent} />
                  </td>
                  <td className="px-4 py-4 text-sm text-slate-700">
                    <FleetRuntimeUpstreamSummary agent={agent} />
                  </td>
                  <td className="rounded-r-3xl px-4 py-4 text-sm text-slate-700">
                    {new Date(agent.last_seen_at).toLocaleString()}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>

      <FleetDetailDrawer
        agent={selectedAgent}
        instances={instances}
        open={Boolean(selectedAgent)}
        onOpenChange={(open) => {
          if (!open) {
            setSelectedAgent(null);
          }
        }}
      />
    </>
  );
}

function JobsPage() {
  const jobsQuery = useQuery({ queryKey: ["jobs"], queryFn: () => apiClient.jobs() });
  const agentsQuery = useQuery({ queryKey: ["agents"], queryFn: () => apiClient.agents() });
  const queryClient = useQueryClient();
  const [selectedAgentID, setSelectedAgentID] = useState("");
  const [ttlSeconds, setTTLSeconds] = useState(60);

  const createJobMutation = useMutation({
    mutationFn: () =>
      apiClient.createJob({
        action: "runtime.reload",
        target_agent_ids: [selectedAgentID],
        idempotency_key: `reload-${Date.now()}`,
        ttl_seconds: ttlSeconds
      }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["jobs"] });
    }
  });

  if (jobsQuery.isLoading || agentsQuery.isLoading) {
    return <CenteredMessage title="Loading actions" description="Collecting the latest action history and available servers." />;
  }

  const agents = agentsQuery.data ?? [];
  const jobs = jobsQuery.data ?? [];

  return (
    <div className="grid gap-6 xl:grid-cols-[0.8fr,1.2fr]">
      <section className="rounded-[32px] border border-white/70 bg-white/85 p-6 shadow-[0_20px_60px_rgba(37,46,68,0.08)]">
        <p className="text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">Quick action</p>
        <h3 className="mt-2 text-2xl font-semibold tracking-tight text-slate-950">Reload a Telemt runtime</h3>
        <p className="mt-3 text-sm leading-6 text-slate-600">
          Pick a connected server and send a runtime reload. This is the fastest place to try a routine control action without leaving the panel.
        </p>
        <div className="mt-6 space-y-4">
          <label className="block">
            <span className="mb-2 block text-sm font-medium text-slate-700">Agent</span>
            <select
              className="w-full rounded-2xl border border-slate-200 bg-slate-50 px-4 py-3 text-sm text-slate-900"
              value={selectedAgentID}
              onChange={(event) => setSelectedAgentID(event.target.value)}
            >
              <option value="">{agents.length === 0 ? "Connect a server first" : "Choose a server"}</option>
              {agents.map((agent) => (
                <option key={agent.id} value={agent.id}>
                  {agent.node_name} ({agent.id})
                </option>
              ))}
            </select>
          </label>
          <Field label="TTL seconds" type="number" value={String(ttlSeconds)} onChange={(value) => setTTLSeconds(Number(value))} />
          <button
            type="button"
            className="rounded-2xl bg-slate-950 px-5 py-3 text-sm font-medium text-white transition hover:bg-slate-800 disabled:opacity-60"
            disabled={!selectedAgentID || createJobMutation.isPending}
            onClick={() => createJobMutation.mutate()}
          >
            {createJobMutation.isPending ? "Sending action..." : "Run reload"}
          </button>
          {createJobMutation.error ? (
            <p className="rounded-2xl bg-rose-50 px-4 py-3 text-sm text-rose-700">
              {(createJobMutation.error as Error).message}
            </p>
          ) : null}
        </div>
      </section>

      <section className="rounded-[32px] border border-white/70 bg-white/85 p-6 shadow-[0_20px_60px_rgba(37,46,68,0.08)]">
        <p className="text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">Recent actions</p>
        <h3 className="mt-2 text-2xl font-semibold tracking-tight text-slate-950">What happened most recently</h3>
        <div className="mt-6 space-y-3">
          {jobs.length > 0 ? (
            jobs.map((job) => <JobRow key={job.id} job={job} />)
          ) : (
            <FriendlyEmptyState
              title="No actions yet"
              description="Once you send a control action, the recent history will appear here with the latest status."
            />
          )}
        </div>
      </section>
    </div>
  );
}

function AuditPage() {
  const auditQuery = useQuery({ queryKey: ["audit"], queryFn: () => apiClient.audit() });

  if (auditQuery.isLoading) {
    return <CenteredMessage title="Loading audit feed" description="Collecting immutable operator events." />;
  }

  return (
    <section className="rounded-[32px] border border-white/70 bg-white/85 p-6 shadow-[0_20px_60px_rgba(37,46,68,0.08)]">
      <p className="text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">Audit</p>
      <h3 className="mt-2 text-2xl font-semibold tracking-tight text-slate-950">Recent security and operator activity</h3>
      <div className="mt-6 space-y-3">
        {auditQuery.data && auditQuery.data.length > 0 ? (
          auditQuery.data.map((event) => <AuditRow key={event.id} event={event} />)
        ) : (
          <FriendlyEmptyState
            title="Nothing to review yet"
            description="Security changes, sign-in events, and operator actions will appear here as the panel starts getting used."
          />
        )}
      </div>
    </section>
  );
}

function AgentsPage() {
  const agentsQuery = useQuery({ queryKey: ["agents"], queryFn: () => apiClient.agents() });

  if (agentsQuery.isLoading) {
    return <CenteredMessage title="Loading agents" description="Refreshing the latest server cards." />;
  }

  return (
    agentsQuery.data && agentsQuery.data.length > 0 ? (
      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
        {agentsQuery.data.map((agent) => (
          <div key={agent.id} className="rounded-[28px] border border-white/70 bg-white/85 p-5 shadow-[0_20px_60px_rgba(37,46,68,0.08)]">
            <div className="flex items-start justify-between gap-4">
              <div>
                <p className="text-lg font-semibold text-slate-950">{agent.node_name}</p>
                <p className="mt-1 text-sm text-slate-500">{agent.fleet_group_id || "Ungrouped"}</p>
              </div>
              <span className={`rounded-full px-3 py-1 text-xs uppercase tracking-[0.22em] ${agent.read_only ? "bg-amber-100 text-amber-800" : "bg-emerald-100 text-emerald-800"}`}>
                {agent.read_only ? "Read only" : "Writable"}
              </span>
            </div>
            <dl className="mt-6 grid grid-cols-2 gap-4 text-sm">
              <div>
                <dt className="text-slate-500">Agent ID</dt>
                <dd className="mt-1 font-medium text-slate-950">{agent.id}</dd>
              </div>
              <div>
                <dt className="text-slate-500">Version</dt>
                <dd className="mt-1 font-medium text-slate-950">{agent.version || "unknown"}</dd>
              </div>
            </dl>
          </div>
        ))}
      </div>
    ) : (
      <FriendlyEmptyState
        title="No agent cards yet"
        description="Once your first server connects, each agent will show up here with its current mode and version."
      />
    )
  );
}

function aggregateMetrics(metrics: MetricSnapshot[]) {
  return metrics.map((snapshot) => ({
    label: new Date(snapshot.captured_at).toLocaleTimeString(),
    connectedUsers: snapshot.values.connected_users ?? 0
  }));
}

function JobRow(props: { job: Job }) {
  return (
    <div className="rounded-3xl border border-slate-200 bg-slate-50 p-4">
      <div className="flex items-center justify-between gap-4">
        <div>
          <p className="font-medium text-slate-950">{formatActionLabel(props.job.action)}</p>
          <p className="mt-1 text-sm text-slate-600">{props.job.target_agent_ids.join(", ")}</p>
        </div>
        <span className="rounded-full bg-slate-950 px-3 py-1 text-xs uppercase tracking-[0.22em] text-white">
          {formatStatusLabel(props.job.status)}
        </span>
      </div>
    </div>
  );
}

function FriendlyEmptyState(props: { title: string; description: string }) {
  return (
    <div className="rounded-[28px] border border-dashed border-slate-300 bg-slate-50/80 px-5 py-10 text-center">
      <h4 className="text-lg font-semibold text-slate-950">{props.title}</h4>
      <p className="mt-3 text-sm leading-6 text-slate-600">{props.description}</p>
    </div>
  );
}

function AuditRow(props: { event: AuditEvent }) {
  return (
    <div className="rounded-3xl border border-slate-200 bg-slate-50 p-4">
      <div className="flex items-center justify-between gap-4">
        <div>
          <p className="font-medium text-slate-950">{formatActionLabel(props.event.action)}</p>
          <p className="mt-1 text-sm text-slate-600">{props.event.actor_id || "system"} → {props.event.target_id}</p>
        </div>
        <span className="text-xs uppercase tracking-[0.22em] text-slate-500">
          {new Date(props.event.created_at).toLocaleString()}
        </span>
      </div>
    </div>
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

function CenteredMessage(props: { title: string; description: string }) {
  return (
    <div className="flex min-h-[50vh] items-center justify-center">
      <div className="max-w-lg rounded-[32px] border border-white/70 bg-white/85 p-8 text-center shadow-[0_20px_60px_rgba(37,46,68,0.08)]">
        <h3 className="text-2xl font-semibold tracking-tight text-slate-950">{props.title}</h3>
        <p className="mt-3 text-sm text-slate-600">{props.description}</p>
        <div className="mt-6">
          <Link to="/login" className="text-sm font-medium text-slate-900 underline underline-offset-4">
            Back to sign-in
          </Link>
        </div>
      </div>
    </div>
  );
}

function formatActionLabel(action: string) {
  const labels: Record<string, string> = {
    "agents.enrolled": "Server enrolled",
    "agents.enrollment.create": "Connection token created",
    "agents.enrollment.revoke": "Connection token revoked",
    "auth.totp.disabled": "Two-factor disabled",
    "auth.totp.enabled": "Two-factor enabled",
    "auth.totp.reset_by_admin": "Two-factor reset by admin",
    "jobs.create": "Action created",
    "jobs.result": "Action finished",
    "runtime.reload": "Reload runtime",
    "settings.panel.update": "Panel settings updated",
    "users.create": "Local user created",
    "users.delete": "Local user deleted",
    "users.update": "Local user updated"
  };

  return labels[action] ?? action.replaceAll(".", " / ");
}

function formatStatusLabel(status: string) {
  return status.replaceAll("_", " ");
}
