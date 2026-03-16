import {
  useMutation,
  useQuery,
  useQueryClient,
  type QueryClient,
  type UseMutationResult,
  type UseQueryResult
} from "@tanstack/react-query";
import { useState } from "react";

import {
  apiClient,
  type EnrollmentTokenResponse,
  type LocalUser,
  type MeResponse,
  type TotpStatusResponse,
  type TotpSetupResponse
} from "./lib/api";

export function SettingsPage() {
  const queryClient = useQueryClient();
  const [environmentID, setEnvironmentID] = useState("prod");
  const [fleetGroupID, setFleetGroupID] = useState("default");
  const [ttlSeconds, setTTLSeconds] = useState(600);
  const [securityPassword, setSecurityPassword] = useState("");
  const [securityTotpCode, setSecurityTotpCode] = useState("");

  const meQuery = useQuery({
    queryKey: ["me"],
    queryFn: () => apiClient.me()
  });

  const usersQuery = useQuery({
    queryKey: ["users"],
    queryFn: () => apiClient.users(),
    enabled: meQuery.data?.role === "admin"
  });

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

  const startTotpSetupMutation = useMutation({
    mutationFn: () => apiClient.startTotpSetup(),
    onSuccess: () => {
      setSecurityTotpCode("");
    }
  });

  const enableTotpMutation = useMutation({
    mutationFn: () =>
      apiClient.enableTotp({
        password: securityPassword,
        totp_code: securityTotpCode
      }),
    onSuccess: async () => {
      setSecurityPassword("");
      setSecurityTotpCode("");
      startTotpSetupMutation.reset();
      await invalidateSecurityQueries(queryClient);
    }
  });

  const disableTotpMutation = useMutation({
    mutationFn: () =>
      apiClient.disableTotp({
        password: securityPassword,
        totp_code: securityTotpCode
      }),
    onSuccess: async () => {
      setSecurityPassword("");
      setSecurityTotpCode("");
      startTotpSetupMutation.reset();
      await invalidateSecurityQueries(queryClient);
    }
  });

  const resetUserTotpMutation = useMutation({
    mutationFn: (userID: string) => apiClient.resetUserTotp(userID),
    onSuccess: async () => {
      await invalidateSecurityQueries(queryClient);
    }
  });

  if (meQuery.isLoading) {
    return <SettingsState title="Loading settings" description="Refreshing account and enrollment preferences." />;
  }

  if (meQuery.isError) {
    return <SettingsState title="Settings unavailable" description="The control-plane could not load the current account." />;
  }

  if (!meQuery.data) {
    return <SettingsState title="Settings unavailable" description="The control-plane did not return the current account." />;
  }

  const me = meQuery.data;
  const pendingSetup = startTotpSetupMutation.data;
  const securityError =
    startTotpSetupMutation.error?.message ??
    enableTotpMutation.error?.message ??
    disableTotpMutation.error?.message ??
    null;

  return (
    <div className="space-y-6">
      <section className="rounded-[32px] border border-white/70 bg-white/85 p-6 shadow-[0_20px_60px_rgba(37,46,68,0.08)]">
        <p className="text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">Settings</p>
        <h2 className="mt-2 text-3xl font-semibold tracking-tight text-slate-950">Keep sign-in and server setup close by</h2>
        <p className="mt-3 max-w-3xl text-sm leading-7 text-slate-600">
          Use this space when you want to connect another server, tighten sign-in security, or help someone on your team recover access without leaving the panel.
        </p>
      </section>

      <div className="grid gap-6 xl:grid-cols-[0.9fr,1.1fr]">
        <EnrollmentPanel
          environmentID={environmentID}
          fleetGroupID={fleetGroupID}
          ttlSeconds={ttlSeconds}
          onEnvironmentIDChange={setEnvironmentID}
          onFleetGroupIDChange={setFleetGroupID}
          onTTLSecondsChange={setTTLSeconds}
          tokenMutation={tokenMutation}
        />
        <SecurityPanel
          me={me}
          pendingSetup={pendingSetup}
          securityPassword={securityPassword}
          securityTotpCode={securityTotpCode}
          onSecurityPasswordChange={setSecurityPassword}
          onSecurityTotpCodeChange={setSecurityTotpCode}
          startTotpSetupMutation={startTotpSetupMutation}
          enableTotpMutation={enableTotpMutation}
          disableTotpMutation={disableTotpMutation}
          securityError={securityError}
        />
      </div>

      {me.role === "admin" ? (
        <AdminUsersPanel me={me} usersQuery={usersQuery} resetUserTotpMutation={resetUserTotpMutation} />
      ) : null}
    </div>
  );
}

function EnrollmentPanel(props: {
  environmentID: string;
  fleetGroupID: string;
  ttlSeconds: number;
  onEnvironmentIDChange: (value: string) => void;
  onFleetGroupIDChange: (value: string) => void;
  onTTLSecondsChange: (value: number) => void;
  tokenMutation: UseMutationResult<EnrollmentTokenResponse, Error, void>;
}) {
  return (
    <section className="rounded-[32px] border border-white/70 bg-white/85 p-6 shadow-[0_20px_60px_rgba(37,46,68,0.08)]">
      <p className="text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">Server connection</p>
      <h3 className="mt-2 text-2xl font-semibold tracking-tight text-slate-950">Connect another server</h3>
      <p className="mt-3 text-sm leading-6 text-slate-600">
        Create a fresh token whenever you want to bring another Telemt server into Panvex. The latest token and CA certificate stay right here for quick copying.
      </p>
      <div className="mt-6 space-y-4">
        <Field label="Environment" value={props.environmentID} onChange={props.onEnvironmentIDChange} />
        <Field label="Fleet group" value={props.fleetGroupID} onChange={props.onFleetGroupIDChange} />
        <Field
          label="TTL seconds"
          type="number"
          value={String(props.ttlSeconds)}
          onChange={(value) => props.onTTLSecondsChange(Number(value))}
        />
        <button
          type="button"
          className="rounded-2xl bg-slate-950 px-5 py-3 text-sm font-medium text-white transition hover:bg-slate-800"
          onClick={() => props.tokenMutation.mutate()}
        >
          {props.tokenMutation.isPending ? "Creating token..." : "Create token"}
        </button>
      </div>
      <div className="mt-6">
        {props.tokenMutation.data ? (
          <div className="space-y-4">
            <CopyBlock label="Enrollment token" value={props.tokenMutation.data.value} />
            <CopyBlock label="CA PEM" value={props.tokenMutation.data.ca_pem} />
          </div>
        ) : (
          <p className="text-sm text-slate-600">Create a token here when you want to connect another server through the agent.</p>
        )}
      </div>
    </section>
  );
}

function SecurityPanel(props: {
  me: MeResponse;
  pendingSetup: TotpSetupResponse | undefined;
  securityPassword: string;
  securityTotpCode: string;
  onSecurityPasswordChange: (value: string) => void;
  onSecurityTotpCodeChange: (value: string) => void;
  startTotpSetupMutation: UseMutationResult<TotpSetupResponse, Error, void>;
  enableTotpMutation: UseMutationResult<TotpStatusResponse, Error, void>;
  disableTotpMutation: UseMutationResult<TotpStatusResponse, Error, void>;
  securityError: string | null;
}) {
  return (
    <section className="rounded-[32px] border border-white/70 bg-white/85 p-6 shadow-[0_20px_60px_rgba(37,46,68,0.08)]">
      <p className="text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">Sign-in security</p>
      <h3 className="mt-2 text-2xl font-semibold tracking-tight text-slate-950">Optional two-factor authentication</h3>
      <p className="mt-3 text-sm text-slate-600">
        TOTP is off by default. Turn it on only if you want an extra sign-in check for your account.
      </p>
      <div className="mt-6 rounded-3xl border border-slate-200 bg-slate-50 p-5">
        <div className="flex items-center justify-between gap-4">
          <div>
            <p className="text-sm font-medium text-slate-950">{props.me.username}</p>
            <p className="mt-1 text-sm text-slate-600">
              Two-factor authentication is {props.me.totp_enabled ? "enabled" : "disabled"}.
            </p>
          </div>
          <StatusBadge enabled={props.me.totp_enabled} />
        </div>

        {props.me.totp_enabled ? (
          <div className="mt-5 space-y-4">
            <Field label="Current password" type="password" value={props.securityPassword} onChange={props.onSecurityPasswordChange} />
            <Field
              label="Current TOTP code"
              value={props.securityTotpCode}
              onChange={props.onSecurityTotpCodeChange}
              placeholder="Enter the code from your authenticator"
            />
            {props.securityError ? <ErrorText message={props.securityError} /> : null}
            <button
              type="button"
              className="rounded-2xl bg-rose-600 px-5 py-3 text-sm font-medium text-white transition hover:bg-rose-500"
              onClick={() => props.disableTotpMutation.mutate()}
              disabled={props.disableTotpMutation.isPending}
            >
              {props.disableTotpMutation.isPending ? "Disabling..." : "Disable TOTP"}
            </button>
          </div>
        ) : (
          <div className="mt-5 space-y-4">
            {props.pendingSetup ? (
              <>
                <CopyBlock label="Authenticator secret" value={props.pendingSetup.secret} />
                <CopyBlock label="OTPAuth URL" value={props.pendingSetup.otpauth_url} />
                <Field label="Current password" type="password" value={props.securityPassword} onChange={props.onSecurityPasswordChange} />
                <Field
                  label="Fresh TOTP code"
                  value={props.securityTotpCode}
                  onChange={props.onSecurityTotpCodeChange}
                  placeholder="Enter the code from your authenticator"
                />
                {props.securityError ? <ErrorText message={props.securityError} /> : null}
                <button
                  type="button"
                  className="rounded-2xl bg-slate-950 px-5 py-3 text-sm font-medium text-white transition hover:bg-slate-800"
                  onClick={() => props.enableTotpMutation.mutate()}
                  disabled={props.enableTotpMutation.isPending}
                >
                  {props.enableTotpMutation.isPending ? "Enabling..." : "Enable TOTP"}
                </button>
              </>
            ) : (
              <>
                <p className="text-sm text-slate-600">
                  Start setup to get a secret for your authenticator app. You will confirm it with your password and a fresh code before TOTP is enabled.
                </p>
                {props.securityError ? <ErrorText message={props.securityError} /> : null}
                <button
                  type="button"
                  className="rounded-2xl bg-slate-950 px-5 py-3 text-sm font-medium text-white transition hover:bg-slate-800"
                  onClick={() => props.startTotpSetupMutation.mutate()}
                  disabled={props.startTotpSetupMutation.isPending}
                >
                  {props.startTotpSetupMutation.isPending ? "Preparing..." : "Start TOTP setup"}
                </button>
              </>
            )}
          </div>
        )}
      </div>
    </section>
  );
}

function AdminUsersPanel(props: {
  me: MeResponse;
  usersQuery: UseQueryResult<LocalUser[], Error>;
  resetUserTotpMutation: UseMutationResult<void, Error, string>;
}) {
  return (
    <section className="rounded-[32px] border border-white/70 bg-white/85 p-6 shadow-[0_20px_60px_rgba(37,46,68,0.08)]">
      <p className="text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">Local accounts</p>
      <h3 className="mt-2 text-2xl font-semibold tracking-tight text-slate-950">Help someone recover access</h3>
      <p className="mt-3 text-sm leading-6 text-slate-600">
        If a teammate loses access to their authenticator app, you can reset TOTP for that account here. Your own account is intentionally excluded from the quick reset action.
      </p>
      {props.usersQuery.isLoading ? <p className="mt-6 text-sm text-slate-600">Loading local accounts...</p> : null}
      {props.usersQuery.isError ? <ErrorText message={props.usersQuery.error.message} /> : null}
      {props.usersQuery.data ? (
        <div className="mt-6 overflow-x-auto">
          <table className="min-w-full border-separate border-spacing-y-3">
            <thead>
              <tr className="text-left text-xs font-semibold uppercase tracking-[0.22em] text-slate-500">
                <th className="px-4 pb-1">User</th>
                <th className="px-4 pb-1">Role</th>
                <th className="px-4 pb-1">TOTP</th>
                <th className="px-4 pb-1">Action</th>
              </tr>
            </thead>
            <tbody>
              {props.usersQuery.data.map((user) => {
                const canReset = user.id !== props.me.id;
                return (
                  <tr key={user.id} className="rounded-3xl bg-slate-50 text-sm text-slate-700">
                    <td className="rounded-l-3xl px-4 py-4 font-medium text-slate-950">{user.username}</td>
                    <td className="px-4 py-4 capitalize">{user.role}</td>
                    <td className="px-4 py-4">{user.totp_enabled ? "Enabled" : "Disabled"}</td>
                    <td className="rounded-r-3xl px-4 py-4">
                      <button
                        type="button"
                        className="rounded-2xl border border-slate-300 px-4 py-2 text-sm font-medium text-slate-800 transition hover:border-slate-400 hover:bg-white disabled:cursor-not-allowed disabled:opacity-50"
                        onClick={() => props.resetUserTotpMutation.mutate(user.id)}
                        disabled={!canReset || props.resetUserTotpMutation.isPending}
                      >
                        {props.resetUserTotpMutation.isPending && props.resetUserTotpMutation.variables === user.id ? "Resetting..." : "Reset TOTP"}
                      </button>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
          {props.resetUserTotpMutation.isError ? <ErrorText message={props.resetUserTotpMutation.error.message} /> : null}
        </div>
      ) : null}
    </section>
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
        <div className="text-xs font-semibold uppercase tracking-[0.22em] text-slate-500">{props.label}</div>
        <button
          type="button"
          className="rounded-full border border-slate-200 px-3 py-1 text-xs font-semibold uppercase tracking-[0.2em] text-slate-600 transition hover:border-slate-300 hover:text-slate-950"
          onClick={() => void navigator.clipboard.writeText(props.value)}
        >
          Copy
        </button>
      </div>
      <pre className="mt-4 overflow-x-auto whitespace-pre-wrap break-all text-sm text-slate-800">{props.value}</pre>
    </div>
  );
}

function ErrorText(props: { message: string }) {
  return <p className="rounded-2xl bg-rose-50 px-4 py-3 text-sm text-rose-700">{props.message}</p>;
}

function SettingsState(props: { title: string; description: string }) {
  return (
    <div className="rounded-[32px] border border-white/70 bg-white/85 p-8 shadow-[0_20px_60px_rgba(37,46,68,0.08)]">
      <h3 className="text-2xl font-semibold tracking-tight text-slate-950">{props.title}</h3>
      <p className="mt-3 text-sm text-slate-600">{props.description}</p>
    </div>
  );
}

function StatusBadge(props: { enabled: boolean }) {
  return (
    <span
      className={`rounded-full px-3 py-1 text-xs font-semibold uppercase tracking-[0.22em] ${
        props.enabled ? "bg-emerald-100 text-emerald-900" : "bg-slate-200 text-slate-700"
      }`}
    >
      {props.enabled ? "Enabled" : "Disabled"}
    </span>
  );
}

async function invalidateSecurityQueries(queryClient: QueryClient) {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: ["me"] }),
    queryClient.invalidateQueries({ queryKey: ["users"] }),
    queryClient.invalidateQueries({ queryKey: ["audit"] })
  ]);
}
