import { useMutation, useQuery, useQueryClient, type UseMutationResult } from "@tanstack/react-query";
import { useEffect, useMemo, useState, type ReactNode } from "react";

import {
  apiClient,
  configuredRootPath,
  type Agent,
  type EnrollmentTokenListItem,
  type EnrollmentTokenResponse
} from "../lib/api";
import { buildInstallCommand, buildManualInstallCommand, resolvePanelInstallURL } from "./agent-install-command";
import { buildConnectionJourney, resolveConnectedAgentID } from "./agent-install-flow-state";

type AgentInstallFlowProps = {
  initialEnvironmentID: string;
  initialFleetGroupID: string;
  createLabel: string;
  onFinish?: () => void;
};

export function AgentInstallFlow(props: AgentInstallFlowProps) {
  const queryClient = useQueryClient();
  const [environmentID, setEnvironmentID] = useState(props.initialEnvironmentID);
  const [fleetGroupID, setFleetGroupID] = useState(props.initialFleetGroupID);
  const [ttlSeconds, setTTLSeconds] = useState(600);
  const [agentVersion, setAgentVersion] = useState("latest");
  const [trackedTokenValue, setTrackedTokenValue] = useState<string | null>(null);
  const [baselineAgentIDs, setBaselineAgentIDs] = useState<string[]>([]);
  const [connectedAgentID, setConnectedAgentID] = useState<string | null>(null);
  const [showAdvanced, setShowAdvanced] = useState(false);

  useEffect(() => {
    setEnvironmentID(props.initialEnvironmentID);
    setFleetGroupID(props.initialFleetGroupID);
  }, [props.initialEnvironmentID, props.initialFleetGroupID]);

  const tokensQuery = useQuery({
    queryKey: ["enrollment-tokens"],
    queryFn: () => apiClient.listEnrollmentTokens()
  });

  const agentsQuery = useQuery({
    queryKey: ["agents"],
    queryFn: () => apiClient.agents()
  });

  const instancesQuery = useQuery({
    queryKey: ["instances"],
    queryFn: () => apiClient.instances()
  });

  const createTokenMutation = useMutation({
    mutationFn: () =>
      apiClient.createEnrollmentToken({
        environment_id: environmentID,
        fleet_group_id: fleetGroupID,
        ttl_seconds: ttlSeconds
      }),
    onSuccess: async (token) => {
      setTrackedTokenValue(token.value);
      setBaselineAgentIDs((agentsQuery.data ?? []).map((agent) => agent.id));
      setConnectedAgentID(null);
      queryClient.setQueryData<EnrollmentTokenListItem[]>(["enrollment-tokens"], (current) => {
        const next = current ? current.filter((item) => item.value !== token.value) : [];
        return [toEnrollmentTokenListItem(token), ...next];
      });
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["audit"] }),
        queryClient.invalidateQueries({ queryKey: ["control-room"] }),
        queryClient.invalidateQueries({ queryKey: ["enrollment-tokens"] })
      ]);
    }
  });

  const revokeTokenMutation = useMutation({
    mutationFn: (value: string) => apiClient.revokeEnrollmentToken(value),
    onSuccess: async () => {
      setConnectedAgentID(null);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["audit"] }),
        queryClient.invalidateQueries({ queryKey: ["control-room"] }),
        queryClient.invalidateQueries({ queryKey: ["enrollment-tokens"] })
      ]);
    }
  });

  const trackedToken = useMemo(() => {
    return resolveTrackedToken(tokensQuery.data ?? [], trackedTokenValue);
  }, [tokensQuery.data, trackedTokenValue]);

  useEffect(() => {
    if (!trackedToken || trackedToken.status !== "consumed" || connectedAgentID) {
      return;
    }

    const candidateID = resolveConnectedAgentID(agentsQuery.data ?? [], instancesQuery.data ?? [], baselineAgentIDs);
    if (candidateID) {
      setConnectedAgentID(candidateID);
    }
  }, [agentsQuery.data, baselineAgentIDs, connectedAgentID, instancesQuery.data, trackedToken]);

  const connectedAgent = useMemo(() => {
    return (agentsQuery.data ?? []).find((agent) => agent.id === connectedAgentID) ?? null;
  }, [agentsQuery.data, connectedAgentID]);

  const panelURL = trackedToken
    ? resolvePanelInstallURL(trackedToken.panel_url, window.location.origin, configuredRootPath)
    : "";
  const primaryInstallCommand = trackedToken ? buildInstallCommand(panelURL, trackedToken.value, agentVersion) : "";
  const manualInstallCommand = trackedToken ? buildManualInstallCommand(panelURL, trackedToken.value, agentVersion) : "";
  const runtimeEnvExample = buildRuntimeEnvExample();
  const flowError = createTokenMutation.error?.message ?? revokeTokenMutation.error?.message ?? null;

  return (
    <div className="grid gap-6 xl:grid-cols-[0.82fr,1.18fr]">
      <div className="space-y-4">
        <StepCard
          number="1"
          title="Generate a connection token"
          description="Pick the environment and group you want to use for the new server. The installer will take care of the secure bootstrap from there."
        >
          <Field label="Environment" value={environmentID} onChange={setEnvironmentID} />
          <Field label="Group" value={fleetGroupID} onChange={setFleetGroupID} />
          <Field
            label="Token lifetime in seconds"
            type="number"
            value={String(ttlSeconds)}
            onChange={(value) => setTTLSeconds(Number(value) || 600)}
          />
          <Field label="Agent version" value={agentVersion} onChange={setAgentVersion} placeholder="latest" />
          <button
            type="button"
            className="inline-flex rounded-2xl bg-slate-950 px-5 py-3 text-sm font-medium text-white transition hover:bg-slate-800 disabled:opacity-60"
            onClick={() => createTokenMutation.mutate()}
            disabled={createTokenMutation.isPending}
          >
            {createTokenMutation.isPending ? "Preparing installer..." : props.createLabel}
          </button>
          {flowError ? <ErrorText message={flowError} /> : null}
        </StepCard>

        <StepCard
          number="2"
          title="Run the installer on the server"
          description="Use the one-line installer on the Linux host that runs Telemt. It downloads the agent, asks for the local Telemt API settings, bootstraps the identity, and starts the systemd service."
        >
          {trackedToken ? (
            <>
              <CopyBlock label="Install command" value={primaryInstallCommand} />
              <p className="text-xs leading-6 text-slate-500">
                This token stays active until {new Date(trackedToken.expires_at_unix * 1000).toLocaleString()} unless you revoke it first.
              </p>
            </>
          ) : (
            <EmptyStep description="Create a token first and the ready-to-run installer command will appear here." />
          )}
        </StepCard>
      </div>

      <div className="space-y-4">
        <ConnectionStatusCard
          token={trackedToken}
          connectedAgent={connectedAgent}
          revokeTokenMutation={revokeTokenMutation}
          onRevoke={() => {
            if (trackedToken) {
              revokeTokenMutation.mutate(trackedToken.value);
            }
          }}
          onFinish={() => {
            setTrackedTokenValue(null);
            setConnectedAgentID(null);
            setBaselineAgentIDs([]);
            if (props.onFinish) {
              props.onFinish();
            }
          }}
        />

        <div className="rounded-[28px] border border-slate-200 bg-slate-50/90 p-5">
          <div className="flex items-center justify-between gap-4">
            <div>
              <h4 className="text-lg font-semibold text-slate-950">Advanced details</h4>
              <p className="mt-2 text-sm leading-6 text-slate-600">
                Keep the manual bootstrap path nearby if you prefer to install the binary yourself or want a quick reference for the runtime env file.
              </p>
            </div>
            <button
              type="button"
              className="rounded-2xl border border-slate-200 bg-white px-4 py-3 text-sm font-medium text-slate-700 transition hover:border-slate-300 hover:text-slate-950"
              onClick={() => setShowAdvanced((value) => !value)}
            >
              {showAdvanced ? "Hide details" : "Show details"}
            </button>
          </div>
          {showAdvanced ? (
            trackedToken ? (
              <div className="mt-4 space-y-4">
                <CopyBlock label="Manual install and bootstrap" value={manualInstallCommand} />
                <CopyBlock label="/etc/panvex-agent/agent.env" value={runtimeEnvExample} />
                <p className="rounded-2xl bg-white px-4 py-3 text-sm text-slate-600">
                  Token status: <span className="font-medium text-slate-900">{trackedToken.status}</span>. The manual bootstrap command still does not require a saved CA file.
                </p>
              </div>
            ) : (
              <EmptyStep description="Create a token first if you want the matching manual bootstrap command and runtime env example." />
            )
          ) : null}
        </div>
      </div>
    </div>
  );
}

function ConnectionStatusCard(props: {
  token: EnrollmentTokenListItem | null;
  connectedAgent: Agent | null;
  revokeTokenMutation: UseMutationResult<void, Error, string, unknown>;
  onRevoke: () => void;
  onFinish: () => void;
}) {
  if (props.connectedAgent) {
    return (
      <StepCard
        number="3"
        title="Server connected"
        description="The agent finished its first full check-in. You can close this flow or head straight into the connected server."
      >
        <ConnectionJourneyCard
          tokenStatus="consumed"
          connected={true}
          summary="Panvex has the first full runtime signal from the new server."
          tone="emerald"
        />
        <div className="rounded-3xl border border-emerald-200 bg-emerald-50 px-4 py-4 text-sm text-emerald-900">
          <div className="font-semibold">{props.connectedAgent.node_name}</div>
          <div className="mt-2 text-emerald-800">
            {props.connectedAgent.environment_id} / {props.connectedAgent.fleet_group_id}
          </div>
          <div className="mt-2 text-emerald-800">Last seen {new Date(props.connectedAgent.last_seen_at).toLocaleString()}</div>
        </div>
        <button
          type="button"
          className="inline-flex rounded-2xl bg-slate-950 px-5 py-3 text-sm font-medium text-white transition hover:bg-slate-800"
          onClick={props.onFinish}
        >
          Finish
        </button>
      </StepCard>
    );
  }

  if (!props.token) {
    return (
      <StepCard
        number="3"
        title="Wait for the first connection"
        description="As soon as the installer finishes and the agent completes its first runtime check-in, the panel will confirm that the new server is ready."
      >
        <EmptyStep description="Create a token first. The live connection state will appear here right after that." />
      </StepCard>
    );
  }

  if (props.token.status === "revoked") {
    return (
      <StepCard
        number="3"
        title="Connection token revoked"
        description="This token can no longer be used. Generate a new one when you are ready to try again."
      >
        <StatusPill tone="slate">Revoked</StatusPill>
      </StepCard>
    );
  }

  if (props.token.status === "expired") {
    return (
      <StepCard
        number="3"
        title="Connection token expired"
        description="The installer did not finish before the token expired. Generate a fresh token and rerun the install command on the server."
      >
        <StatusPill tone="amber">Expired</StatusPill>
      </StepCard>
    );
  }

  if (props.token.status === "consumed") {
    return (
      <StepCard
        number="3"
        title="Finishing the first check-in"
        description="The bootstrap token has already been consumed. Panvex is waiting for the first full runtime signal from the new agent."
      >
        <ConnectionJourneyCard
          tokenStatus={props.token.status}
          connected={false}
          summary="The secure bootstrap is done. Panvex is waiting for the first runtime signal from the agent."
          tone="sky"
        />
      </StepCard>
    );
  }

  return (
    <StepCard
      number="3"
      title="Waiting for connection"
      description="Run the installer on the target server. This card updates automatically as soon as the agent checks in."
    >
      <div className="space-y-4 rounded-3xl border border-slate-200 bg-white px-4 py-4">
        <ConnectionJourneyCard
          tokenStatus={props.token.status}
          connected={false}
          summary="The installer token is active. Start the setup on the Linux host that runs Telemt."
          tone="sky"
        />
        <button
          type="button"
          className="rounded-2xl border border-slate-200 px-4 py-3 text-sm font-medium text-slate-700 transition hover:border-slate-300 hover:text-slate-950 disabled:opacity-60"
          onClick={props.onRevoke}
          disabled={props.revokeTokenMutation.isPending}
        >
          {props.revokeTokenMutation.isPending ? "Revoking..." : "Revoke token"}
        </button>
      </div>
    </StepCard>
  );
}

function buildRuntimeEnvExample() {
  return ["PANVEX_STATE_FILE=/var/lib/panvex-agent/agent-state.json", "PANVEX_TELEMT_URL=http://127.0.0.1:9091", "PANVEX_TELEMT_AUTH="].join("\n");
}

function resolveTrackedToken(tokens: EnrollmentTokenListItem[], trackedTokenValue: string | null) {
  if (trackedTokenValue) {
    return tokens.find((token) => token.value === trackedTokenValue) ?? null;
  }

  return tokens.find((token) => token.status === "active") ?? null;
}

function toEnrollmentTokenListItem(token: EnrollmentTokenResponse): EnrollmentTokenListItem {
  return {
    value: token.value,
    panel_url: token.panel_url,
    environment_id: token.environment_id,
    fleet_group_id: token.fleet_group_id,
    status: "active",
    issued_at_unix: token.issued_at_unix,
    expires_at_unix: token.expires_at_unix
  };
}

function StepCard(props: { number: string; title: string; description: string; children: ReactNode }) {
  return (
    <div className="rounded-[28px] border border-slate-200 bg-slate-50/90 p-5">
      <div className="flex items-start gap-4">
        <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-2xl bg-slate-950 text-sm font-semibold text-white">
          {props.number}
        </div>
        <div className="min-w-0 flex-1">
          <h4 className="text-lg font-semibold text-slate-950">{props.title}</h4>
          <p className="mt-2 text-sm leading-6 text-slate-600">{props.description}</p>
          <div className="mt-4 space-y-4">{props.children}</div>
        </div>
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
        className="w-full rounded-2xl border border-slate-200 bg-white px-4 py-3 text-sm text-slate-900"
        value={props.value}
        placeholder={props.placeholder}
        onChange={(event) => props.onChange(event.target.value)}
      />
    </label>
  );
}

function CopyBlock(props: { label: string; value: string }) {
  return (
    <div className="rounded-3xl border border-slate-200 bg-white p-4">
      <div className="flex items-center justify-between gap-4">
        <p className="text-sm font-medium text-slate-900">{props.label}</p>
        <button
          type="button"
          className="rounded-full border border-slate-200 px-3 py-1 text-xs font-semibold uppercase tracking-[0.2em] text-slate-600 transition hover:border-slate-300 hover:text-slate-950"
          onClick={() => void navigator.clipboard.writeText(props.value)}
        >
          Copy
        </button>
      </div>
      <pre className="mt-3 overflow-x-auto whitespace-pre-wrap break-all rounded-2xl bg-slate-950 px-4 py-4 text-xs leading-6 text-slate-100">
        {props.value}
      </pre>
    </div>
  );
}

function EmptyStep(props: { description: string }) {
  return (
    <div className="rounded-3xl border border-dashed border-slate-300 bg-white px-4 py-5 text-sm leading-6 text-slate-500">
      {props.description}
    </div>
  );
}

function ErrorText(props: { message: string }) {
  return <p className="rounded-2xl bg-rose-50 px-4 py-3 text-sm text-rose-700">{props.message}</p>;
}

function ConnectionJourneyCard(props: {
  tokenStatus: "active" | "consumed" | "expired" | "revoked";
  connected: boolean;
  summary: string;
  tone: "emerald" | "sky";
}) {
  const steps = buildConnectionJourney(props.tokenStatus, props.connected);

  return (
    <div className={`rounded-3xl border px-4 py-4 ${props.tone === "emerald" ? "border-emerald-200 bg-emerald-50" : "border-sky-200 bg-sky-50/70"}`}>
      <div className="grid gap-3 md:grid-cols-[minmax(0,1fr)_40px_minmax(0,1fr)_40px_minmax(0,1fr)] md:items-center">
        <ConnectionJourneyStepCard step={steps[0]} tone={props.tone} />
        <ConnectionJourneyLine fromState={steps[0].state} toState={steps[1].state} />
        <ConnectionJourneyStepCard step={steps[1]} tone={props.tone} />
        <ConnectionJourneyLine fromState={steps[1].state} toState={steps[2].state} />
        <ConnectionJourneyStepCard step={steps[2]} tone={props.tone} />
      </div>
      <p className={`mt-4 text-sm leading-6 ${props.tone === "emerald" ? "text-emerald-900" : "text-slate-700"}`}>{props.summary}</p>
    </div>
  );
}

function ConnectionJourneyStepCard(props: {
  step: ReturnType<typeof buildConnectionJourney>[number];
  tone: "emerald" | "sky";
}) {
  const stepClass =
    props.step.state === "done"
      ? "border-emerald-200 bg-white text-emerald-900"
      : props.step.state === "active"
        ? props.tone === "emerald"
          ? "border-emerald-300 bg-white text-emerald-900"
          : "border-sky-300 bg-white text-slate-950"
        : "border-slate-200 bg-white/80 text-slate-500";
  const badgeClass =
    props.step.state === "done"
      ? "border-emerald-200 bg-emerald-500 text-white"
      : props.step.state === "active"
        ? props.tone === "emerald"
          ? "border-emerald-200 bg-emerald-500 text-white"
          : "border-sky-200 bg-sky-500 text-white"
        : "border-slate-200 bg-slate-100 text-slate-500";

  return (
    <div className={`rounded-[24px] border px-4 py-4 ${stepClass}`}>
      <div className="flex items-start gap-3">
        <div className="relative mt-0.5">
          {props.step.state === "active" ? (
            <span className={`absolute inset-0 rounded-full opacity-35 ${props.tone === "emerald" ? "bg-emerald-300" : "bg-sky-300"} animate-ping`} />
          ) : null}
          <span className={`relative flex h-8 w-8 items-center justify-center rounded-full border text-xs font-semibold uppercase tracking-[0.2em] ${badgeClass}`}>
            {props.step.state === "done" ? "OK" : props.step.key.charAt(0)}
          </span>
        </div>
        <div className="min-w-0">
          <div className="text-sm font-semibold">{props.step.label}</div>
          <div className="mt-1 text-xs leading-5">{props.step.detail}</div>
        </div>
      </div>
    </div>
  );
}

function ConnectionJourneyLine(props: {
  fromState: "idle" | "active" | "done";
  toState: "idle" | "active" | "done";
}) {
  const lineClass =
    props.fromState === "done"
      ? "bg-emerald-400"
      : props.fromState === "active" || props.toState === "active"
        ? "bg-sky-300"
        : "bg-slate-200";
  const pulseClass = props.toState === "active" ? "after:absolute after:inset-y-0 after:left-0 after:w-1/2 after:animate-pulse after:rounded-full after:bg-white/50" : "";

  return (
    <div className="hidden md:flex md:justify-center">
      <div className={`relative h-1 w-full rounded-full ${lineClass} ${pulseClass}`} />
    </div>
  );
}

function StatusPill(props: { tone: "amber" | "slate"; children: ReactNode }) {
  const toneClass = props.tone === "amber" ? "bg-amber-100 text-amber-900" : "bg-slate-200 text-slate-700";

  return <span className={`inline-flex rounded-full px-3 py-1 text-xs font-semibold uppercase tracking-[0.22em] ${toneClass}`}>{props.children}</span>;
}
