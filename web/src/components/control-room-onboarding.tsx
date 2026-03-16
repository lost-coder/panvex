import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState, type ReactNode } from "react";

import {
  apiClient,
  type ControlRoomResponse,
  type EnrollmentTokenResponse
} from "../lib/api";

type ControlRoomOnboardingProps = {
  onboarding: ControlRoomResponse["onboarding"];
};

export function ControlRoomOnboarding(props: ControlRoomOnboardingProps) {
  const queryClient = useQueryClient();
  const [expanded, setExpanded] = useState(props.onboarding.needs_first_server);
  const [environmentID, setEnvironmentID] = useState(props.onboarding.suggested_environment_id);
  const [fleetGroupID, setFleetGroupID] = useState(props.onboarding.suggested_fleet_group_id);
  const [ttlSeconds, setTTLSeconds] = useState(600);

  useEffect(() => {
    setExpanded(props.onboarding.needs_first_server);
  }, [props.onboarding.needs_first_server]);

  useEffect(() => {
    setEnvironmentID(props.onboarding.suggested_environment_id);
    setFleetGroupID(props.onboarding.suggested_fleet_group_id);
  }, [props.onboarding.suggested_environment_id, props.onboarding.suggested_fleet_group_id]);

  const tokenMutation = useMutation({
    mutationFn: () =>
      apiClient.createEnrollmentToken({
        environment_id: environmentID,
        fleet_group_id: fleetGroupID,
        ttl_seconds: ttlSeconds
      }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["audit"] });
      await queryClient.invalidateQueries({ queryKey: ["control-room"] });
    }
  });

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
            Create a short-lived enrollment token, save the CA certificate on the server that runs your agent, and then start the agent with the ready-to-edit command below.
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

      <div className="mt-6 grid gap-6 xl:grid-cols-[0.82fr,1.18fr]">
        <div className="space-y-4">
          <StepCard number="1" title="Create a connection token" description="Pick the default environment and group for the new server. You can change them later if your layout grows.">
            <Field label="Environment" value={environmentID} onChange={setEnvironmentID} />
            <Field label="Group" value={fleetGroupID} onChange={setFleetGroupID} />
            <Field label="Token lifetime in seconds" type="number" value={String(ttlSeconds)} onChange={(value) => setTTLSeconds(Number(value) || 600)} />
            <button
              type="button"
              className="inline-flex rounded-2xl bg-slate-950 px-5 py-3 text-sm font-medium text-white transition hover:bg-slate-800 disabled:opacity-60"
              onClick={() => tokenMutation.mutate()}
              disabled={tokenMutation.isPending}
            >
              {tokenMutation.isPending ? "Creating token..." : props.onboarding.needs_first_server ? "Create first token" : "Create another token"}
            </button>
            {tokenMutation.error ? <ErrorText message={tokenMutation.error.message} /> : null}
          </StepCard>
        </div>

        <div className="space-y-4">
          <StepCard
            number="2"
            title="Save the CA certificate on the server"
            description="Create a local file called control-plane-ca.pem and paste the certificate contents into it."
          >
            {tokenMutation.data ? (
              <CopyBlock label="control-plane-ca.pem" value={tokenMutation.data.ca_pem} />
            ) : (
              <EmptyStep description="The CA certificate appears here right after you create the token." />
            )}
          </StepCard>

          <StepCard
            number="3"
            title="Start the agent"
            description="The command below uses the token and suggested defaults. Replace the Telemt authorization placeholder with the local secret from the same server."
          >
            {tokenMutation.data ? (
              <>
                <CopyBlock label="Enrollment token" value={tokenMutation.data.value} />
                <CopyBlock label="Agent start command" value={buildAgentCommand(tokenMutation.data)} />
                <p className="text-xs leading-6 text-slate-500">
                  Token expires at {new Date(tokenMutation.data.expires_at_unix * 1000).toLocaleString()}. If your gRPC listener uses a different address, replace the first flag before you start the agent.
                </p>
              </>
            ) : (
              <EmptyStep description="Create a token first and the ready-to-edit start command will appear here." />
            )}
          </StepCard>
        </div>
      </div>
    </section>
  );
}

function buildAgentCommand(token: EnrollmentTokenResponse) {
  const host = window.location.hostname || "127.0.0.1";

  return [
    "./agent \\",
    `  -gateway-addr \"${host}:8443\" \\`,
    '  -gateway-server-name "control-plane.panvex.internal" \\',
    '  -ca-file "./control-plane-ca.pem" \\',
    `  -enrollment-token \"${token.value}\" \\`,
    `  -environment-id \"${token.environment_id}\" \\`,
    `  -fleet-group-id \"${token.fleet_group_id}\" \\`,
    '  -telemt-url "http://127.0.0.1:8080" \\',
    '  -telemt-auth "<local-telemt-authorization>"'
  ].join("\n");
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

function Field(props: { label: string; value: string; onChange: (value: string) => void; type?: string }) {
  return (
    <label className="block">
      <span className="mb-2 block text-sm font-medium text-slate-700">{props.label}</span>
      <input
        type={props.type ?? "text"}
        className="w-full rounded-2xl border border-slate-200 bg-white px-4 py-3 text-sm text-slate-900"
        value={props.value}
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
