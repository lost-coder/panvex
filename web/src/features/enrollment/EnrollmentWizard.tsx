// Phase-7 v2 — checklist restored, Metrics URL surfaced as a primary
// field (Telemt ships with metrics disabled by default, so we can't
// hide the knob inside Advanced), fleet group optional.
import { useEffect, useState } from "react";

import { cn } from "@/ui/lib/cn";
import { Button } from "@/ui/base/button";
import { CopyButton } from "@/ui/primitives/CopyButton";
import { FormField } from "@/ui/base/form-field";
import { Input } from "@/ui/base/input";
import { Select } from "@/ui/base/select";
import { StepIndicator } from "@/ui/primitives/StepIndicator";
import type { EnrollmentWizardProps } from "@/shared/api/types-pages/pages";

const TTL_PRESETS = [
  { value: 3600, label: "1 hour" },
  { value: 21600, label: "6 hours" },
  { value: 86400, label: "24 hours" },
];
const STEPS = ["Configure", "Install", "Connect"];

// ─── Step 1: Configure ───────────────────────────────────────────────

function ConfigureStep(props: EnrollmentWizardProps) {
  const {
    fleetGroups,
    nodeName,
    selectedFleetGroup,
    tokenTtl,
    onNodeNameChange,
    onFleetGroupChange,
    onTokenTtlChange,
    onGenerateToken,
    advancedOptions,
    onAdvancedOptionsChange,
    loading,
    error,
  } = props;

  const [customTtl, setCustomTtl] = useState(false);
  const [touched, setTouched] = useState<{ nodeName?: boolean; ttl?: boolean }>({});

  const nodeNameError = !nodeName.trim() ? "Node name is required." : undefined;
  const ttlError = tokenTtl <= 0 ? "Token lifetime must be greater than zero." : undefined;
  const hasError = Boolean(nodeNameError || ttlError);

  const handleGenerate = () => {
    if (hasError) {
      setTouched({ nodeName: true, ttl: true });
      return;
    }
    onGenerateToken();
  };

  return (
    <div className="flex flex-col gap-4">
      <FormField label="Node name" variant="uppercase" required>
        <Input
          type="text"
          placeholder="e.g. prod-eu-west-1"
          value={nodeName}
          onChange={(e) => onNodeNameChange(e.target.value)}
          onBlur={() => setTouched((t) => ({ ...t, nodeName: true }))}
          disabled={loading}
          aria-invalid={touched.nodeName && !!nodeNameError}
          aria-describedby={touched.nodeName && nodeNameError ? "enroll-node-err" : undefined}
        />
        {touched.nodeName && nodeNameError && (
          <div id="enroll-node-err" className="text-xs text-status-error mt-1">
            {nodeNameError}
          </div>
        )}
      </FormField>

      {/* Fleet group is optional — empty string means the backend
          attaches the new agent to the default scope. Select still
          renders when groups exist so operators can opt-in. */}
      <FormField label="Fleet group" variant="uppercase">
        <Select
          value={selectedFleetGroup}
          options={[
            { value: "", label: "— none (default scope) —" },
            ...fleetGroups.map((g) => ({
              value: g.id,
              label: `${g.name ?? g.label ?? g.id} (${g.nodeCount ?? g.agentCount ?? 0} nodes)`,
            })),
          ]}
          onChange={onFleetGroupChange}
        />
        <div className="text-[11px] font-mono text-fg-muted mt-1">
          Leave empty to add the node without a group — it'll land in the default scope.
        </div>
      </FormField>

      <FormField label="Token lifetime" variant="uppercase">
        <div className="flex flex-wrap gap-2" role="group" aria-label="Token lifetime presets">
          {TTL_PRESETS.map((p) => {
            const pressed = !customTtl && tokenTtl === p.value;
            return (
              <button
                key={p.value}
                type="button"
                aria-pressed={pressed}
                onClick={() => {
                  setCustomTtl(false);
                  onTokenTtlChange(p.value);
                  setTouched((t) => ({ ...t, ttl: true }));
                }}
                className={cn(
                  "px-3 py-1.5 rounded-xs text-xs transition-colors",
                  pressed
                    ? "bg-accent text-white"
                    : "border border-border text-fg-muted hover:text-fg",
                )}
              >
                {p.label}
              </button>
            );
          })}
          <button
            type="button"
            aria-pressed={customTtl}
            onClick={() => {
              setCustomTtl(true);
              setTouched((t) => ({ ...t, ttl: true }));
            }}
            className={cn(
              "px-3 py-1.5 rounded-xs text-xs transition-colors",
              customTtl
                ? "bg-accent text-white"
                : "border border-border text-fg-muted hover:text-fg",
            )}
          >
            Custom
          </button>
        </div>
        {customTtl && (
          <Input
            type="number"
            min={1}
            placeholder="Seconds"
            value={tokenTtl}
            onChange={(e) => onTokenTtlChange(Number(e.target.value))}
            onBlur={() => setTouched((t) => ({ ...t, ttl: true }))}
            aria-invalid={touched.ttl && !!ttlError}
            className="mt-2 w-32"
          />
        )}
        {touched.ttl && ttlError && (
          <div className="text-xs text-status-error mt-1">{ttlError}</div>
        )}
      </FormField>

      {/* Telemt endpoints + auth are always visible — no collapsible
          fold. Defaults work for a local Telemt on the same host. The
          metrics-disabled-by-default warning lives on step 2's
          checklist so we don't repeat it per-field here. */}
      {advancedOptions && onAdvancedOptionsChange && (
        <div className="flex flex-col gap-3">
          <FormField label="Telemt API URL" variant="uppercase">
            <Input
              value={advancedOptions.telemtUrl}
              onChange={(e) =>
                onAdvancedOptionsChange({ ...advancedOptions, telemtUrl: e.target.value })
              }
              className="font-mono text-xs"
            />
          </FormField>
          <FormField label="Telemt metrics URL" variant="uppercase">
            <Input
              value={advancedOptions.telemtMetricsUrl}
              onChange={(e) =>
                onAdvancedOptionsChange({
                  ...advancedOptions,
                  telemtMetricsUrl: e.target.value,
                })
              }
              placeholder="http://127.0.0.1:8081"
              className="font-mono text-xs"
            />
          </FormField>
          <FormField label="Telemt auth header" variant="uppercase">
            <Input
              value={advancedOptions.telemtAuth}
              onChange={(e) =>
                onAdvancedOptionsChange({ ...advancedOptions, telemtAuth: e.target.value })
              }
              placeholder="optional"
              className="font-mono text-xs"
            />
          </FormField>
          {/* Opt-in relaxation of the agent's "https required unless
              loopback" guard. Surface the warning tone so operators
              who don't read the description can still see this is not
              the default. */}
          <label className="flex items-start gap-2 rounded-xs border border-status-warn/30 bg-status-warn/5 p-3 cursor-pointer">
            <input
              type="checkbox"
              className="mt-0.5 h-4 w-4 accent-[var(--color-status-warn)] cursor-pointer"
              checked={advancedOptions.insecureTransport}
              onChange={(e) =>
                onAdvancedOptionsChange({
                  ...advancedOptions,
                  insecureTransport: e.target.checked,
                })
              }
            />
            <span className="flex flex-col gap-0.5">
              <span className="text-xs font-medium text-status-warn">
                Allow plaintext over private network
              </span>
              <span className="text-[11px] text-fg-muted leading-snug">
                Passes <code className="font-mono">--insecure-transport</code> so the
                agent accepts an http:// panel URL on a non-loopback host.
                Only safe on a VPN or other trusted link — bootstrap
                exchanges the agent private key in cleartext.
              </span>
            </span>
          </label>
        </div>
      )}

      <div className="rounded-xs bg-accent/8 border border-accent/20 p-3 text-xs text-accent">
        <strong>Note:</strong> Telemt (mtproto-proxy) must already be running on the target server.
      </div>

      {error && <div className="text-xs text-status-error">{error}</div>}

      <Button onClick={handleGenerate} disabled={loading}>
        {loading ? "Generating…" : "Generate token →"}
      </Button>
    </div>
  );
}

// ─── Step 2: Install ─────────────────────────────────────────────────

function InstallStep({
  installCommand,
  advancedOptions,
  onInstallConfirm,
  onBack,
  tokenValue,
  tokenExpiresInSecs,
}: EnrollmentWizardProps) {
  const [showTroubleshooting, setShowTroubleshooting] = useState(false);
  const expiresMin = Math.round(tokenExpiresInSecs / 60);

  const requirements: Array<{
    label: string;
    detail?: string;
    tone?: "default" | "warn";
  }> = [
    { label: "Linux host (amd64 / arm64)" },
    { label: "Root privileges (sudo)" },
    { label: "systemd service manager" },
    { label: "curl or wget for bootstrap" },
    { label: "Telemt (mtproto-proxy) running locally" },
    {
      // Highlighted in amber because Telemt ships with metrics OFF —
      // operators routinely miss this and then wonder why per-client
      // traffic / IP / quota counters stay empty.
      tone: "warn",
      label: "Enable Telemt metrics export (disabled by default)",
      detail: advancedOptions?.telemtMetricsUrl
        ? `agent will poll ${advancedOptions.telemtMetricsUrl}`
        : undefined,
    },
  ];

  return (
    <div className="flex flex-col gap-4">
      <div className="rounded-xs bg-bg-card border border-divider p-3">
        <div className="text-[10px] font-medium text-fg-muted uppercase tracking-wider mb-2">
          Before you run the command
        </div>
        <div className="flex flex-col gap-1.5 text-xs text-fg">
          {requirements.map((r) => (
            <div key={r.label} className="flex items-start gap-2">
              <span
                className={cn(
                  "mt-0.5",
                  r.tone === "warn" ? "text-status-warn" : "text-status-ok",
                )}
              >
                {r.tone === "warn" ? "!" : "✓"}
              </span>
              <div className="flex flex-col min-w-0">
                <span className={cn(r.tone === "warn" && "text-status-warn font-medium")}>
                  {r.label}
                </span>
                {r.detail && (
                  <span className="text-[11px] font-mono text-fg-muted">{r.detail}</span>
                )}
              </div>
            </div>
          ))}
        </div>
      </div>

      <div>
        <div className="flex justify-between items-center mb-1.5">
          <label className="text-[10px] font-medium text-fg-muted uppercase tracking-wider">
            Install command
          </label>
          <CopyButton text={installCommand} />
        </div>
        <pre className="rounded-xs bg-bg border border-border p-3 text-xs font-mono text-fg leading-relaxed whitespace-pre-wrap break-all overflow-x-auto">
          {installCommand}
        </pre>
      </div>

      <button
        type="button"
        onClick={() => setShowTroubleshooting((v) => !v)}
        className="text-xs text-fg-muted hover:text-fg text-left"
      >
        {showTroubleshooting ? "▾" : "▸"} Troubleshooting
      </button>
      {showTroubleshooting && (
        <div className="rounded-xs border border-divider p-3 flex flex-col gap-3 text-xs">
          <div>
            <div className="font-medium text-fg">Connection refused</div>
            <div className="text-fg-muted">
              Check Telemt is running:{" "}
              <code className="bg-black/30 px-1 rounded">
                curl http://127.0.0.1:9091/v1/health
              </code>
            </div>
          </div>
          <div>
            <div className="font-medium text-fg">Metrics empty after connect</div>
            <div className="text-fg-muted">
              Telemt ships with metrics off. Enable the metrics exporter in your Telemt
              config and confirm{" "}
              <code className="bg-black/30 px-1 rounded">
                curl {advancedOptions?.telemtMetricsUrl || "http://127.0.0.1:8081"}
              </code>{" "}
              answers before bootstrapping.
            </div>
          </div>
          <div>
            <div className="font-medium text-fg">Permission denied</div>
            <div className="text-fg-muted">
              Run with <code className="bg-black/30 px-1 rounded">sudo</code> — root is required
              for systemd.
            </div>
          </div>
          <div>
            <div className="font-medium text-fg">Token expired</div>
            <div className="text-fg-muted">
              Go back and generate a new token. Tokens are single-use and time-limited.
            </div>
          </div>
        </div>
      )}

      <div className="flex items-center justify-between text-xs text-fg-muted rounded-xs bg-bg-card border border-divider px-3 py-2">
        <span>
          Token: <span className="font-mono">{tokenValue.slice(0, 12)}…</span>
        </span>
        <span>
          Expires in: <span className="text-status-warn">{expiresMin} min</span>
        </span>
      </div>

      <div className="flex gap-2">
        <Button variant="ghost" onClick={onBack}>
          ← Back
        </Button>
        <Button className="flex-1" onClick={onInstallConfirm}>
          I've run the command →
        </Button>
      </div>
    </div>
  );
}

// ─── Step 3: Connect ─────────────────────────────────────────────────

function ConnectStep({
  connectionStatus,
  connectedAgent,
  tokenValue,
  tokenExpiresInSecs,
  onViewDetails,
  onCancel,
}: EnrollmentWizardProps) {
  const allDone =
    connectionStatus.bootstrap === "done" &&
    connectionStatus.grpcConnect === "done" &&
    connectionStatus.firstData === "done";

  useEffect(() => {
    if (allDone && connectedAgent && onViewDetails) {
      const id = window.setTimeout(() => onViewDetails(), 300);
      return () => window.clearTimeout(id);
    }
    return undefined;
  }, [allDone, connectedAgent, onViewDetails]);

  const expiresMin = Math.round(tokenExpiresInSecs / 60);
  const stages: Array<{
    key: string;
    label: string;
    detail: string;
    state: "pending" | "waiting" | "done";
  }> = [
    {
      key: "bootstrap",
      label: "Bootstrap",
      detail: "Agent received enrollment certificate",
      state: connectionStatus.bootstrap,
    },
    {
      key: "grpcConnect",
      label: "Gateway connected",
      detail: "gRPC stream to control-plane established",
      state: connectionStatus.grpcConnect,
    },
    {
      key: "firstData",
      label: "First snapshot",
      detail: "Runtime telemetry received",
      state: connectionStatus.firstData,
    },
  ];

  return (
    <div className="flex flex-col gap-4">
      <div className="relative pl-5">
        <span aria-hidden="true" className="absolute top-1 bottom-1 left-[6px] w-px bg-divider" />
        {stages.map((s) => {
          const dotColor =
            s.state === "done"
              ? "bg-status-ok"
              : s.state === "waiting"
                ? "bg-status-warn"
                : "bg-fg-faint";
          return (
            <div key={s.key} className="relative py-3 first:pt-1 last:pb-1">
              <span
                aria-hidden="true"
                className={cn(
                  "absolute -left-[12px] top-[14px] h-2 w-2 rounded-full z-10",
                  dotColor,
                )}
              />
              {s.state === "waiting" && (
                <span
                  aria-hidden="true"
                  className="absolute -left-[14px] top-[12px] h-3 w-3 rounded-full border-2 border-status-warn border-t-transparent animate-spin"
                />
              )}
              <div className="flex items-baseline gap-3">
                <span
                  className={cn(
                    "text-sm font-medium",
                    s.state === "pending" ? "text-fg-muted" : "text-fg",
                  )}
                >
                  {s.label}
                </span>
                <span className="text-[10px] font-mono uppercase tracking-wider text-fg-muted">
                  {s.state}
                </span>
              </div>
              <div className="text-[11px] font-mono text-fg-muted">{s.detail}</div>
            </div>
          );
        })}
      </div>

      {allDone && connectedAgent && (
        <div className="rounded-xs bg-status-ok/8 border border-status-ok/25 p-3 text-xs text-status-ok">
          <strong>{connectedAgent.id}</strong> is online. Redirecting to the server page…
        </div>
      )}

      <div className="flex items-center justify-between text-xs text-fg-muted rounded-xs bg-bg-card border border-divider px-3 py-2">
        <span>
          Token: <span className="font-mono">{tokenValue.slice(0, 12)}…</span>
        </span>
        <span>
          Expires in: <span className="text-status-warn">{expiresMin} min</span>
        </span>
      </div>

      <Button variant="ghost" onClick={onCancel}>
        Cancel
      </Button>
    </div>
  );
}

// ─── Main ────────────────────────────────────────────────────────────

export function EnrollmentWizard(props: EnrollmentWizardProps) {
  return (
    <div className="flex flex-col gap-5">
      <div className="flex flex-col gap-1">
        <h3 className="text-title">Add server node</h3>
        <p className="text-sm text-fg-muted">
          {props.step === 1 && "Pick a node name; we'll mint a one-shot token."}
          {props.step === 2 && "Run this command on the target Linux server as root."}
          {props.step === 3 && "Waiting for the agent to come online."}
        </p>
      </div>

      <StepIndicator steps={STEPS} current={props.step - 1} />

      {props.step === 1 && <ConfigureStep {...props} />}
      {props.step === 2 && <InstallStep {...props} />}
      {props.step === 3 && <ConnectStep {...props} />}
    </div>
  );
}
