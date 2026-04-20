import { useState } from "react";
import { cn } from "@/ui/lib/cn";
import { StepIndicator } from "@/ui/primitives/StepIndicator";
import { CopyButton } from "@/ui/primitives/CopyButton";
import { Button } from "@/ui/base/button";
import { Input } from "@/ui/base/input";
import { Select } from "@/ui/base/select";
import { FormField } from "@/ui/base/form-field";
import type { EnrollmentWizardProps } from "@/shared/api/types-pages/pages";

const TTL_PRESETS = [
  { value: 3600, label: "1 hour" },
  { value: 21600, label: "6 hours" },
  { value: 86400, label: "24 hours" },
];

const STEPS = ["Configure", "Install", "Connect"];

// ─── Step 1: Configure ───────────────────────────────────────────────────────

function ConfigureStep({
  fleetGroups,
  nodeName,
  selectedFleetGroup,
  tokenTtl,
  onNodeNameChange,
  onFleetGroupChange,
  onTokenTtlChange,
  onGenerateToken,
  loading,
  error,
}: EnrollmentWizardProps) {
  const [customTtl, setCustomTtl] = useState(false);
  const [touched, setTouched] = useState<{ nodeName?: boolean; ttl?: boolean; fleet?: boolean }>(
    {},
  );

  // P2-UX-07: inline validation messages. These do not block keystroke-by-keystroke
  // — they only surface once a field has been touched (blurred) or on submit attempt,
  // so operators don't see "required" while they are actively typing.
  const nodeNameError = !nodeName.trim() ? "Node name is required." : undefined;
  const ttlError = tokenTtl <= 0 ? "Token lifetime must be greater than zero." : undefined;
  const fleetError = !selectedFleetGroup ? "Fleet group is required." : undefined;
  const hasError = Boolean(nodeNameError || ttlError || fleetError);

  const handleGenerate = () => {
    if (hasError) {
      // Force-reveal every validation message so the operator sees what's missing.
      setTouched({ nodeName: true, ttl: true, fleet: true });
      return;
    }
    onGenerateToken();
  };

  return (
    <div className="flex flex-col gap-4">
      <FormField label="Node Name" variant="uppercase" required>
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

      <FormField label="Fleet Group" variant="uppercase" required>
        <Select
          value={selectedFleetGroup}
          options={fleetGroups.map((g) => ({
            value: g.id,
            label: `${g.name ?? g.label ?? g.id} (${g.nodeCount ?? g.agentCount ?? 0} nodes)`,
          }))}
          onChange={(v) => {
            onFleetGroupChange(v);
            setTouched((t) => ({ ...t, fleet: true }));
          }}
        />
        {touched.fleet && fleetError && (
          <div className="text-xs text-status-error mt-1">{fleetError}</div>
        )}
      </FormField>

      <FormField label="Token Lifetime" variant="uppercase">
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

      <div className="rounded-xs bg-accent/8 border border-accent/20 p-3 text-xs text-accent">
        <strong>Note:</strong> Telemt (mtproto-proxy) must already be running on the target server.
      </div>

      {error && <div className="text-xs text-status-error">{error}</div>}

      <Button onClick={handleGenerate} disabled={loading}>
        {loading ? "Generating..." : "Generate Token →"}
      </Button>
    </div>
  );
}

// ─── Step 2: Install ─────────────────────────────────────────────────────────

function InstallStep({
  installCommand,
  advancedOptions,
  onAdvancedOptionsChange,
  onInstallConfirm,
  onBack,
  tokenValue,
  tokenExpiresInSecs,
}: EnrollmentWizardProps) {
  const [showAdvanced, setShowAdvanced] = useState(false);
  const [showTroubleshooting, setShowTroubleshooting] = useState(false);
  const expiresMin = Math.round(tokenExpiresInSecs / 60);

  return (
    <div className="flex flex-col gap-4">
      <div className="rounded-xs bg-bg-card border border-border p-3">
        <div className="text-[10px] font-medium text-fg-muted uppercase tracking-wider mb-2">
          Requirements
        </div>
        <div className="flex flex-col gap-1 text-xs text-fg">
          {[
            "Linux (amd64 / arm64)",
            "Root privileges (sudo)",
            "systemd",
            "curl or wget",
            "Telemt running on the server",
          ].map((r) => (
            <div key={r}>
              <span className="text-status-ok">✓</span> {r}
            </div>
          ))}
        </div>
      </div>

      <div>
        <div className="flex justify-between items-center mb-1.5">
          <label className="text-[10px] font-medium text-fg-muted uppercase tracking-wider">
            Install Command
          </label>
          <CopyButton text={installCommand} />
        </div>
        <pre className="rounded-xs bg-bg border border-border p-3 text-xs font-mono text-fg leading-relaxed whitespace-pre-wrap break-all overflow-x-auto">
          {installCommand}
        </pre>
      </div>

      <button
        type="button"
        onClick={() => setShowAdvanced(!showAdvanced)}
        className="text-xs text-fg-muted hover:text-fg text-left"
      >
        {showAdvanced ? "▾" : "▸"} Advanced options
      </button>
      {showAdvanced && advancedOptions && onAdvancedOptionsChange && (
        <div className="rounded-xs border border-border p-3 flex flex-col gap-3">
          <FormField label="Telemt API URL" variant="compact">
            <Input
              value={advancedOptions.telemtUrl}
              onChange={(e) =>
                onAdvancedOptionsChange({ ...advancedOptions, telemtUrl: e.target.value })
              }
              className="font-mono text-xs"
            />
          </FormField>
          <FormField label="Telemt Auth Header" variant="compact">
            <Input
              value={advancedOptions.telemtAuth}
              onChange={(e) =>
                onAdvancedOptionsChange({ ...advancedOptions, telemtAuth: e.target.value })
              }
              placeholder="optional"
              className="font-mono text-xs"
            />
          </FormField>
        </div>
      )}

      <button
        type="button"
        onClick={() => setShowTroubleshooting(!showTroubleshooting)}
        className="text-xs text-fg-muted hover:text-fg text-left"
      >
        {showTroubleshooting ? "▾" : "▸"} Troubleshooting
      </button>
      {showTroubleshooting && (
        <div className="rounded-xs border border-border p-3 flex flex-col gap-3 text-xs">
          <div>
            <div className="font-medium text-fg">Connection refused</div>
            <div className="text-fg-muted">
              Check Telemt is running:{" "}
              <code className="bg-black/30 px-1 rounded">curl http://127.0.0.1:9091/v1/health</code>
            </div>
          </div>
          <div>
            <div className="font-medium text-fg">Permission denied</div>
            <div className="text-fg-muted">
              Run with <code className="bg-black/30 px-1 rounded">sudo</code> — root is required for
              systemd.
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

      <div className="flex items-center justify-between text-xs text-fg-muted">
        <span>
          Token: <span className="font-mono">{tokenValue.slice(0, 12)}...</span>
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

// ─── Step 3: Connect ─────────────────────────────────────────────────────────

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
  const expiresMin = Math.round(tokenExpiresInSecs / 60);

  const statusSteps = [
    {
      key: "bootstrap",
      label: "Bootstrap complete",
      sub: "Agent received certificate",
      status: connectionStatus.bootstrap,
    },
    {
      key: "grpcConnect",
      label: "Connected to panel",
      sub: "gRPC connection established",
      status: connectionStatus.grpcConnect,
    },
    {
      key: "firstData",
      label: "First data received",
      sub: "Runtime snapshot uploaded",
      status: connectionStatus.firstData,
    },
  ];

  if (allDone && connectedAgent) {
    return (
      <div className="flex flex-col items-center gap-4 py-4">
        <div className="w-14 h-14 rounded-full bg-status-ok/12 flex items-center justify-center">
          <span className="text-3xl text-status-ok">✓</span>
        </div>
        <div className="text-center">
          <h3 className="text-title">Node Connected</h3>
          <p className="text-sm text-fg-muted mt-1">Online and reporting data.</p>
        </div>
        <div className="w-full rounded-xs bg-bg-card border border-border p-3 grid grid-cols-1 sm:grid-cols-2 gap-2 text-xs">
          <div>
            <span className="text-fg-muted">Agent ID:</span>{" "}
            <span className="font-mono">{connectedAgent.id}</span>
          </div>
          <div>
            <span className="text-fg-muted">Version:</span>{" "}
            <span className="font-mono">{connectedAgent.version}</span>
          </div>
          <div>
            <span className="text-fg-muted">Fleet:</span> {connectedAgent.fleetGroup}
          </div>
          <div>
            <span className="text-fg-muted">Cert expires:</span> {connectedAgent.certExpiresAt}
          </div>
        </div>
        <Button className="w-full" onClick={onViewDetails}>
          Open Server Details →
        </Button>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col gap-1">
        {statusSteps.map((s) => (
          <div
            key={s.key}
            className={cn(
              "flex items-center gap-3 p-3 rounded-xs border",
              s.status === "done"
                ? "bg-status-ok/5 border-status-ok/15"
                : s.status === "waiting"
                  ? "bg-status-warn/5 border-status-warn/15"
                  : "bg-bg-card border-border",
            )}
          >
            <div
              className={cn(
                "w-7 h-7 rounded-full flex items-center justify-center",
                s.status === "done"
                  ? "bg-status-ok/15"
                  : s.status === "waiting"
                    ? "bg-status-warn/15"
                    : "bg-bg-card",
              )}
            >
              {s.status === "done" ? (
                <span className="text-status-ok text-sm">✓</span>
              ) : s.status === "waiting" ? (
                <div className="w-2.5 h-2.5 rounded-full border-2 border-status-warn border-t-transparent animate-spin" />
              ) : (
                <span className="text-fg-muted text-sm">○</span>
              )}
            </div>
            <div>
              <div
                className={cn(
                  "text-sm font-medium",
                  s.status === "pending" ? "text-fg-muted" : "text-fg",
                )}
              >
                {s.label}
              </div>
              <div className="text-caption">{s.sub}</div>
            </div>
          </div>
        ))}
      </div>

      <div className="flex items-center justify-between text-xs text-fg-muted rounded-xs bg-bg-card border border-border px-3 py-2">
        <span>
          Token: <span className="font-mono">{tokenValue.slice(0, 12)}...</span>
        </span>
        <span>
          Expires: <span className="text-status-warn">{expiresMin} min</span>
        </span>
      </div>

      <Button variant="ghost" onClick={onCancel}>
        Cancel
      </Button>
    </div>
  );
}

// ─── Main ────────────────────────────────────────────────────────────────────

export function EnrollmentWizard(props: EnrollmentWizardProps) {
  return (
    <div className="flex flex-col gap-5">
      <div>
        <h3 className="text-title">Add Server Node</h3>
        <p className="text-sm text-fg-muted mt-0.5">
          {props.step === 1 && "Generate an enrollment token for a new Telemt server."}
          {props.step === 2 && "Run this command on your Linux server as root."}
          {props.step === 3 && "Waiting for your agent to connect."}
        </p>
      </div>

      <StepIndicator steps={STEPS} current={props.step - 1} />

      {props.step === 1 && <ConfigureStep {...props} />}
      {props.step === 2 && <InstallStep {...props} />}
      {props.step === 3 && <ConnectStep {...props} />}
    </div>
  );
}
