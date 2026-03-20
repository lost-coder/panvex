import { useMutation, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { apiClient, type MeResponse } from "../lib/api";
import { toggleAccordionSection } from "./settings-accordion-state";
import { AccordionSection, CopyBlock, ErrorText, Field, StatusBadge } from "./settings-shared";

export function SecuritySettingsPanel(props: { me: MeResponse }) {
  const queryClient = useQueryClient();
  const [password, setPassword] = useState("");
  const [totpCode, setTotpCode] = useState("");
  const [expandedSection, setExpandedSection] = useState<string | null>(null);

  const startTotpSetupMutation = useMutation({
    mutationFn: () => apiClient.startTotpSetup(),
    onSuccess: () => {
      setTotpCode("");
    }
  });

  const enableTotpMutation = useMutation({
    mutationFn: () =>
      apiClient.enableTotp({
        password,
        totp_code: totpCode
      }),
    onSuccess: async () => {
      setPassword("");
      setTotpCode("");
      startTotpSetupMutation.reset();
      await invalidateSecurityQueries(queryClient);
    }
  });

  const disableTotpMutation = useMutation({
    mutationFn: () =>
      apiClient.disableTotp({
        password,
        totp_code: totpCode
      }),
    onSuccess: async () => {
      setPassword("");
      setTotpCode("");
      startTotpSetupMutation.reset();
      await invalidateSecurityQueries(queryClient);
    }
  });

  const securityError =
    startTotpSetupMutation.error?.message ??
    enableTotpMutation.error?.message ??
    disableTotpMutation.error?.message ??
    null;

  return (
    <AccordionSection
      title="Optional two-factor authentication"
      description="TOTP stays optional for local accounts. Turn it on only if you want an extra sign-in check for your own access."
      open={expandedSection === "totp"}
      onToggle={() => setExpandedSection((currentSection) => toggleAccordionSection(currentSection, "totp"))}
      trailing={<StatusBadge enabled={props.me.totp_enabled} />}
    >
      <div className="app-card-muted rounded-3xl p-5">
        <div className="flex items-center justify-between gap-4">
          <div>
            <p className="text-sm font-medium text-[var(--app-text-primary)]">{props.me.username}</p>
            <p className="mt-1 text-sm text-[var(--app-text-secondary)]">
              Two-factor authentication is {props.me.totp_enabled ? "enabled" : "disabled"}.
            </p>
          </div>
        </div>

        {props.me.totp_enabled ? (
          <div className="mt-5 space-y-4">
            <Field label="Current password" type="password" value={password} onChange={setPassword} />
            <Field
              label="Current TOTP code"
              value={totpCode}
              onChange={setTotpCode}
              placeholder="Enter the code from your authenticator"
            />
            {securityError ? <ErrorText message={securityError} /> : null}
            <button
              type="button"
              className="rounded-2xl bg-rose-600 px-5 py-3 text-sm font-medium text-white transition hover:bg-rose-500 disabled:opacity-60"
              onClick={() => disableTotpMutation.mutate()}
              disabled={disableTotpMutation.isPending}
            >
              {disableTotpMutation.isPending ? "Disabling..." : "Disable TOTP"}
            </button>
          </div>
        ) : (
          <div className="mt-5 space-y-4">
            {startTotpSetupMutation.data ? (
              <>
                <CopyBlock label="Authenticator secret" value={startTotpSetupMutation.data.secret} />
                <CopyBlock label="OTPAuth URL" value={startTotpSetupMutation.data.otpauth_url} />
                <Field label="Current password" type="password" value={password} onChange={setPassword} />
                <Field
                  label="Fresh TOTP code"
                  value={totpCode}
                  onChange={setTotpCode}
                  placeholder="Enter the code from your authenticator"
                />
                {securityError ? <ErrorText message={securityError} /> : null}
                <button
                  type="button"
                  className="app-button-primary rounded-2xl text-sm font-medium"
                  onClick={() => enableTotpMutation.mutate()}
                  disabled={enableTotpMutation.isPending}
                >
                  {enableTotpMutation.isPending ? "Enabling..." : "Enable TOTP"}
                </button>
              </>
            ) : (
              <>
                <p className="text-sm text-[var(--app-text-secondary)]">
                  Start setup to get a secret for your authenticator app. You will confirm it with your password and a fresh code before TOTP is enabled.
                </p>
                {securityError ? <ErrorText message={securityError} /> : null}
                <button
                  type="button"
                  className="app-button-primary rounded-2xl text-sm font-medium"
                  onClick={() => startTotpSetupMutation.mutate()}
                  disabled={startTotpSetupMutation.isPending}
                >
                  {startTotpSetupMutation.isPending ? "Preparing..." : "Start TOTP setup"}
                </button>
              </>
            )}
          </div>
        )}
      </div>
    </AccordionSection>
  );
}

async function invalidateSecurityQueries(queryClient: QueryClient) {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: ["me"] }),
    queryClient.invalidateQueries({ queryKey: ["users"] }),
    queryClient.invalidateQueries({ queryKey: ["audit"] })
  ]);
}
