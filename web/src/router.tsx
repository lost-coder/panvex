import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Link,
  Navigate,
  Outlet,
  createRootRouteWithContext,
  createRoute,
  createRouter
} from "@tanstack/react-router";
import { useMemo, useState } from "react";
import { Area, AreaChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";

import { AppShell } from "./components/app-shell";
import { FleetDetailDrawer } from "./components/fleet-detail-drawer";
import {
  apiClient,
  type Agent,
  type AuditEvent,
  type Job,
  type MetricSnapshot
} from "./lib/api";

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
  path: "/",
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

const settingsRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/settings",
  component: SettingsPage
});

const routeTree = rootRoute.addChildren([
  loginRoute,
  shellRoute.addChildren([overviewRoute, fleetRoute, jobsRoute, auditRoute, agentsRoute, settingsRoute])
]);

export const router = createRouter({
  routeTree,
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
    return <CenteredMessage title="Loading control room" description="Rebuilding operator context." />;
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
          Authenticate with your local operator account to inspect fleet health, dispatch runtime actions, and monitor Telemt nodes in one place.
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
          <Field label="TOTP code" value={totpCode} onChange={setTotpCode} placeholder="Required for operator and admin" />
          {error ? <p className="rounded-2xl bg-rose-50 px-4 py-3 text-sm text-rose-700">{error}</p> : null}
          <button
            type="submit"
            className="inline-flex rounded-2xl bg-slate-950 px-5 py-3 text-sm font-medium text-white transition hover:bg-slate-800 disabled:opacity-60"
            disabled={loginMutation.isPending}
          >
            {loginMutation.isPending ? "Authenticating..." : "Open dashboard"}
          </button>
        </form>
      </div>
    </div>
  );
}

function OverviewPage() {
  const fleetQuery = useQuery({ queryKey: ["fleet"], queryFn: () => apiClient.fleet() });
  const metricsQuery = useQuery({ queryKey: ["metrics"], queryFn: () => apiClient.metrics() });
  const auditQuery = useQuery({ queryKey: ["audit"], queryFn: () => apiClient.audit() });

  if (fleetQuery.isLoading || auditQuery.isLoading) {
    return <CenteredMessage title="Loading overview" description="Collecting the latest fleet summary." />;
  }

  if (fleetQuery.isError || auditQuery.isError) {
    return <CenteredMessage title="Overview unavailable" description="The dashboard could not load its latest control-plane data." />;
  }

  const fleet = fleetQuery.data!;
  const auditEvents = auditQuery.data ?? [];
  const chartData = aggregateMetrics(metricsQuery.data ?? []);

  return (
    <div className="space-y-6">
      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
        <MetricCard label="Agents online" value={fleet.online_agents.toString()} tone="emerald" />
        <MetricCard label="Agents degraded" value={fleet.degraded_agents.toString()} tone="amber" />
        <MetricCard label="Agents offline" value={fleet.offline_agents.toString()} tone="rose" />
        <MetricCard label="Instances" value={fleet.total_instances.toString()} tone="slate" />
      </div>

      <div className="grid gap-6 xl:grid-cols-[1.2fr,0.8fr]">
        <section className="rounded-[32px] border border-white/70 bg-white/85 p-6 shadow-[0_20px_60px_rgba(37,46,68,0.08)]">
          <p className="text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">Metric drift</p>
          <h3 className="mt-2 text-2xl font-semibold tracking-tight text-slate-950">Connected users trend</h3>
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
        </section>

        <section className="rounded-[32px] border border-white/70 bg-white/85 p-6 shadow-[0_20px_60px_rgba(37,46,68,0.08)]">
          <p className="text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">Latest events</p>
          <h3 className="mt-2 text-2xl font-semibold tracking-tight text-slate-950">Operator and security feed</h3>
          <div className="mt-6 space-y-3">
            {auditEvents.slice(0, 6).map((event) => (
              <AuditRow key={event.id} event={event} />
            ))}
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
    return <CenteredMessage title="Loading fleet" description="Building the latest inventory table." />;
  }

  if (agentsQuery.isError || instancesQuery.isError) {
    return <CenteredMessage title="Fleet unavailable" description="The fleet inventory could not be loaded." />;
  }

  const agents = agentsQuery.data ?? [];
  const instances = instancesQuery.data ?? [];

  return (
    <>
      <div className="rounded-[32px] border border-white/70 bg-white/85 p-6 shadow-[0_20px_60px_rgba(37,46,68,0.08)]">
        <div className="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
          <div>
            <p className="text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">Fleet grid</p>
            <h3 className="mt-2 text-2xl font-semibold tracking-tight text-slate-950">Nodes and Telemt runtimes</h3>
          </div>
          <p className="max-w-xl text-sm text-slate-600">
            Dense inventory remains the primary workspace: select a node to inspect local Telemt instances without leaving the table.
          </p>
        </div>
        <div className="mt-6 overflow-x-auto">
          <table className="min-w-full border-separate border-spacing-y-3">
            <thead>
              <tr className="text-left text-xs font-semibold uppercase tracking-[0.22em] text-slate-500">
                <th className="px-4 pb-1">Node</th>
                <th className="px-4 pb-1">Environment</th>
                <th className="px-4 pb-1">Version</th>
                <th className="px-4 pb-1">Mode</th>
                <th className="px-4 pb-1">Last seen</th>
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
                    <div className="font-medium text-slate-950">{agent.node_name}</div>
                    <div className="mt-1 text-sm text-slate-500">{agent.id}</div>
                  </td>
                  <td className="px-4 py-4 text-sm text-slate-700">{agent.environment_id} / {agent.fleet_group_id}</td>
                  <td className="px-4 py-4 text-sm text-slate-700">{agent.version || "unknown"}</td>
                  <td className="px-4 py-4">
                    <span className={`rounded-full px-3 py-1 text-xs uppercase tracking-[0.22em] ${agent.read_only ? "bg-amber-100 text-amber-800" : "bg-emerald-100 text-emerald-800"}`}>
                      {agent.read_only ? "Read only" : "Writable"}
                    </span>
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
    return <CenteredMessage title="Loading jobs" description="Replaying the command queue." />;
  }

  const agents = agentsQuery.data ?? [];
  const jobs = jobsQuery.data ?? [];

  return (
    <div className="grid gap-6 xl:grid-cols-[0.8fr,1.2fr]">
      <section className="rounded-[32px] border border-white/70 bg-white/85 p-6 shadow-[0_20px_60px_rgba(37,46,68,0.08)]">
        <p className="text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">Dispatch</p>
        <h3 className="mt-2 text-2xl font-semibold tracking-tight text-slate-950">Runtime reload</h3>
        <div className="mt-6 space-y-4">
          <label className="block">
            <span className="mb-2 block text-sm font-medium text-slate-700">Agent</span>
            <select
              className="w-full rounded-2xl border border-slate-200 bg-slate-50 px-4 py-3 text-sm text-slate-900"
              value={selectedAgentID}
              onChange={(event) => setSelectedAgentID(event.target.value)}
            >
              <option value="">Select agent</option>
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
            {createJobMutation.isPending ? "Dispatching..." : "Dispatch runtime.reload"}
          </button>
          {createJobMutation.error ? (
            <p className="rounded-2xl bg-rose-50 px-4 py-3 text-sm text-rose-700">
              {(createJobMutation.error as Error).message}
            </p>
          ) : null}
        </div>
      </section>

      <section className="rounded-[32px] border border-white/70 bg-white/85 p-6 shadow-[0_20px_60px_rgba(37,46,68,0.08)]">
        <p className="text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">Queue</p>
        <h3 className="mt-2 text-2xl font-semibold tracking-tight text-slate-950">Recent jobs</h3>
        <div className="mt-6 space-y-3">
          {jobs.map((job) => (
            <JobRow key={job.id} job={job} />
          ))}
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
      <h3 className="mt-2 text-2xl font-semibold tracking-tight text-slate-950">Immutable event trail</h3>
      <div className="mt-6 space-y-3">
        {auditQuery.data?.map((event) => (
          <AuditRow key={event.id} event={event} />
        ))}
      </div>
    </section>
  );
}

function AgentsPage() {
  const agentsQuery = useQuery({ queryKey: ["agents"], queryFn: () => apiClient.agents() });

  if (agentsQuery.isLoading) {
    return <CenteredMessage title="Loading agents" description="Refreshing live node status." />;
  }

  return (
    <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
      {agentsQuery.data?.map((agent) => (
        <div key={agent.id} className="rounded-[28px] border border-white/70 bg-white/85 p-5 shadow-[0_20px_60px_rgba(37,46,68,0.08)]">
          <div className="flex items-start justify-between gap-4">
            <div>
              <p className="text-lg font-semibold text-slate-950">{agent.node_name}</p>
              <p className="mt-1 text-sm text-slate-500">{agent.environment_id} / {agent.fleet_group_id}</p>
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
  );
}

function SettingsPage() {
  const queryClient = useQueryClient();
  const [environmentID, setEnvironmentID] = useState("prod");
  const [fleetGroupID, setFleetGroupID] = useState("default");
  const [ttlSeconds, setTTLSeconds] = useState(600);

  const tokenMutation = useMutation({
    mutationFn: () =>
      apiClient.createEnrollmentToken({
        environment_id: environmentID,
        fleet_group_id: fleetGroupID,
        ttl_seconds: ttlSeconds
      }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["audit"] });
    }
  });

  return (
    <div className="grid gap-6 xl:grid-cols-[0.9fr,1.1fr]">
      <section className="rounded-[32px] border border-white/70 bg-white/85 p-6 shadow-[0_20px_60px_rgba(37,46,68,0.08)]">
        <p className="text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">Enrollment</p>
        <h3 className="mt-2 text-2xl font-semibold tracking-tight text-slate-950">Issue agent bootstrap token</h3>
        <div className="mt-6 space-y-4">
          <Field label="Environment" value={environmentID} onChange={setEnvironmentID} />
          <Field label="Fleet group" value={fleetGroupID} onChange={setFleetGroupID} />
          <Field label="TTL seconds" type="number" value={String(ttlSeconds)} onChange={(value) => setTTLSeconds(Number(value))} />
          <button
            type="button"
            className="rounded-2xl bg-slate-950 px-5 py-3 text-sm font-medium text-white transition hover:bg-slate-800"
            onClick={() => tokenMutation.mutate()}
          >
            {tokenMutation.isPending ? "Issuing..." : "Create token"}
          </button>
        </div>
      </section>

      <section className="rounded-[32px] border border-white/70 bg-white/85 p-6 shadow-[0_20px_60px_rgba(37,46,68,0.08)]">
        <p className="text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">Bootstrap artifact</p>
        <h3 className="mt-2 text-2xl font-semibold tracking-tight text-slate-950">Latest enrollment package</h3>
        {tokenMutation.data ? (
          <div className="mt-6 space-y-4">
            <CopyBlock label="Enrollment token" value={tokenMutation.data.value} />
            <CopyBlock label="CA PEM" value={tokenMutation.data.ca_pem} />
          </div>
        ) : (
          <p className="mt-6 text-sm text-slate-600">Issue a token to generate the bootstrap package for a new agent.</p>
        )}
      </section>
    </div>
  );
}

function aggregateMetrics(metrics: MetricSnapshot[]) {
  return metrics.map((snapshot) => ({
    label: new Date(snapshot.captured_at).toLocaleTimeString(),
    connectedUsers: snapshot.values.connected_users ?? 0
  }));
}

function MetricCard(props: { label: string; value: string; tone: "emerald" | "amber" | "rose" | "slate" }) {
  const toneClass = {
    emerald: "from-emerald-500/18 to-emerald-200/10 text-emerald-900",
    amber: "from-amber-500/18 to-amber-200/10 text-amber-900",
    rose: "from-rose-500/18 to-rose-200/10 text-rose-900",
    slate: "from-slate-900/10 to-slate-200/10 text-slate-900"
  }[props.tone];

  return (
    <div className={`rounded-[28px] border border-white/70 bg-gradient-to-br ${toneClass} p-5 shadow-[0_20px_60px_rgba(37,46,68,0.08)]`}>
      <div className="text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">{props.label}</div>
      <div className="mt-5 text-4xl font-semibold tracking-tight">{props.value}</div>
    </div>
  );
}

function JobRow(props: { job: Job }) {
  return (
    <div className="rounded-3xl border border-slate-200 bg-slate-50 p-4">
      <div className="flex items-center justify-between gap-4">
        <div>
          <p className="font-medium text-slate-950">{props.job.action}</p>
          <p className="mt-1 text-sm text-slate-600">{props.job.target_agent_ids.join(", ")}</p>
        </div>
        <span className="rounded-full bg-slate-950 px-3 py-1 text-xs uppercase tracking-[0.22em] text-white">
          {props.job.status}
        </span>
      </div>
    </div>
  );
}

function AuditRow(props: { event: AuditEvent }) {
  return (
    <div className="rounded-3xl border border-slate-200 bg-slate-50 p-4">
      <div className="flex items-center justify-between gap-4">
        <div>
          <p className="font-medium text-slate-950">{props.event.action}</p>
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

function CopyBlock(props: { label: string; value: string }) {
  return (
    <div className="rounded-3xl border border-slate-200 bg-slate-50 p-4">
      <div className="text-xs font-semibold uppercase tracking-[0.22em] text-slate-500">{props.label}</div>
      <pre className="mt-4 overflow-x-auto whitespace-pre-wrap break-all text-sm text-slate-800">{props.value}</pre>
    </div>
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
            Return to login
          </Link>
        </div>
      </div>
    </div>
  );
}
