import { useMutation, useQuery, useQueryClient, type UseMutationResult } from "@tanstack/react-query";
import { useEffect, useMemo, useState, type ReactNode } from "react";

import {
  apiClient,
  type Agent,
  type EnrollmentTokenListItem,
  type EnrollmentTokenResponse
} from "../lib/api";
import { resolveConnectedAgentID } from "./agent-install-flow-state";

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

  const primaryInstallCommand = trackedToken ? buildInstallCommand(trackedToken.value, agentVersion) : "";
  const manualInstallCommand = trackedToken ? buildManualInstallCommand(trackedToken.value, agentVersion) : "";
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
        <WaitingCard status="Finishing connection" />
      </StepCard>
    );
  }

  return (
    <StepCard
      number="3"
      title="Waiting for connection"
      description="Run the installer on the target server. This card updates automatically as soon as the agent checks in."
    >
      <div className="flex items-center justify-between gap-4 rounded-3xl border border-slate-200 bg-white px-4 py-4">
        <WaitingCard status="Installer token is active" />
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

function buildInstallCommand(tokenValue: string, agentVersion: string) {
  const versionFlag = agentVersion.trim() !== "" && agentVersion !== "latest" ? ` --version ${agentVersion.trim()}` : "";

  return [
    "curl -fsSL https://github.com/panvex/panvex/releases/latest/download/install-agent.sh | \\",
    "  sudo sh -s -- \\",
    `    --panel-url ${currentPanelURL()} \\`,
    `    --enrollment-token ${tokenValue}${versionFlag}`
  ].join("\n");
}

function buildManualInstallCommand(tokenValue: string, agentVersion: string) {
  const releaseSegment = agentVersion.trim() !== "" && agentVersion !== "latest" ? `download/${agentVersion.trim()}` : "latest/download";

  return [
    `curl -fsSL -o panvex-agent.tar.gz https://github.com/panvex/panvex/releases/${releaseSegment}/panvex-agent-linux-<amd64|arm64>.tar.gz`,
    "tar -xzf panvex-agent.tar.gz",
    "sudo install -m 0755 panvex-agent /usr/local/bin/panvex-agent",
    "sudo /usr/local/bin/panvex-agent bootstrap \\",
    `  -panel-url ${currentPanelURL()} \\`,
    `  -enrollment-token ${tokenValue} \\`,
    '  -state-file /var/lib/panvex-agent/agent-state.json'
  ].join("\n");
}

function buildRuntimeEnvExample() {
  return ["PANVEX_STATE_FILE=/var/lib/panvex-agent/agent-state.json", "PANVEX_TELEMT_URL=http://127.0.0.1:9091", "PANVEX_TELEMT_AUTH="].join("\n");
}

function currentPanelURL() {
  return window.location.origin || "https://panel.example.com";
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

function WaitingCard(props: { status: string }) {
  return (
    <div className="flex items-center gap-4">
      <div className="flex gap-1.5">
        <span className="h-2.5 w-2.5 animate-pulse rounded-full bg-sky-500 [animation-delay:-0.3s]" />
        <span className="h-2.5 w-2.5 animate-pulse rounded-full bg-sky-500 [animation-delay:-0.15s]" />
        <span className="h-2.5 w-2.5 animate-pulse rounded-full bg-sky-500" />
      </div>
      <div className="text-sm font-medium text-slate-900">{props.status}</div>
    </div>
  );
}

function StatusPill(props: { tone: "amber" | "slate"; children: ReactNode }) {
  const toneClass = props.tone === "amber" ? "bg-amber-100 text-amber-900" : "bg-slate-200 text-slate-700";

  return <span className={`inline-flex rounded-full px-3 py-1 text-xs font-semibold uppercase tracking-[0.22em] ${toneClass}`}>{props.children}</span>;
}
