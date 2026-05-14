import { useState } from "react";

import { cn } from "@/ui/lib/cn";
import { Button } from "@/ui/base/button";
import { FormField } from "@/ui/base/form-field";
import { Input } from "@/ui/base/input";
import { Select } from "@/ui/base/select";
import type { EnrollmentWizardProps } from "@/shared/api/types-pages/pages";

const TTL_PRESETS = [
  { value: 3600, label: "1 hour" },
  { value: 21600, label: "6 hours" },
  { value: 86400, label: "24 hours" },
];

export function ConfigureStep(props: Readonly<EnrollmentWizardProps>) {
  const {
    fleetGroups,
    nodeName,
    selectedFleetGroup,
    tokenTtl,
    onNodeNameChange,
    onFleetGroupChange,
    onCreateFleetGroup,
    onTokenTtlChange,
    onGenerateToken,
    advancedOptions,
    onAdvancedOptionsChange,
    mode,
    onModeChange,
    dialAddress,
    onDialAddressChange,
    scriptSource,
    onScriptSourceChange,
    loading,
    error,
  } = props;

  // PR-3c: Advanced section is collapsed by default — operators rarely
  // tune Telemt URLs or flip insecure-transport; surfacing them up-front
  // crowded the wizard.
  const [advancedOpen, setAdvancedOpen] = useState(false);

  // The mode picker only appears when the container threads both pieces
  // of state — older callers that don't supply mode/onModeChange keep
  // rendering the inbound-only form unchanged.
  const showModePicker = mode !== undefined && onModeChange !== undefined;
  const effectiveMode: "inbound" | "outbound" = mode ?? "inbound";
  const isOutbound = effectiveMode === "outbound";

  const showSourceToggle =
    scriptSource !== undefined && onScriptSourceChange !== undefined;

  const [customTtl, setCustomTtl] = useState(false);
  const [touched, setTouched] = useState<{
    nodeName?: boolean;
    ttl?: boolean;
    dialAddress?: boolean;
  }>({});

  const nodeNameError = nodeName.trim() ? undefined : "Node name is required.";
  const ttlError = tokenTtl <= 0 ? "Token lifetime must be greater than zero." : undefined;
  // Outbound requires a host:port the panel can dial. We don't validate
  // the full RFC here — the server enforces `net.SplitHostPort` — but a
  // visible colon catches the most common typo before the round-trip.
  const dialError = isOutbound
    ? !dialAddress || !dialAddress.trim()
      ? "Dial address is required for outbound mode."
      : !/^\S+:\d+$/.test(dialAddress.trim())
        ? "Dial address must be host:port (e.g. vps.example.com:8443)."
        : undefined
    : undefined;
  const hasError = Boolean(nodeNameError || (!isOutbound && ttlError) || dialError);

  const handleGenerate = () => {
    if (hasError) {
      setTouched({ nodeName: true, ttl: true, dialAddress: true });
      return;
    }
    onGenerateToken();
  };

  return (
    <div className="flex flex-col gap-4">
      {showModePicker && (
        <FormField label="Transport mode" variant="uppercase">
          <div
            role="radiogroup"
            aria-label="Agent transport mode"
            className="inline-flex rounded-xs border border-border p-0.5 bg-bg w-full"
          >
            {([
              { value: "inbound", label: "Agent → Panel" },
              { value: "outbound", label: "Panel → Agent" },
            ] as const).map((opt) => {
              const selected = effectiveMode === opt.value;
              return (
                <button
                  key={opt.value}
                  type="button"
                  role="radio"
                  aria-checked={selected}
                  aria-label={opt.value === "inbound" ? "Agent connects to panel" : "Panel connects to agent"}
                  onClick={() => onModeChange?.(opt.value)}
                  disabled={loading}
                  className={cn(
                    "flex-1 px-3 py-1.5 rounded-xs text-xs transition-colors",
                    selected
                      ? "bg-accent text-white"
                      : "text-fg-muted hover:text-fg",
                  )}
                >
                  {opt.label}
                </button>
              );
            })}
          </div>
          <div className="text-[11px] text-fg-muted mt-1 leading-snug">
            {effectiveMode === "inbound"
              ? "Agent dials the panel. Use when the panel is internet-reachable from the agent host."
              : "Panel dials the agent on its public host:port. Use when the panel is firewalled (private network / VPN)."}
          </div>
        </FormField>
      )}

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
        <div className="flex gap-2">
          <Select
            className="flex-1"
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
          {onCreateFleetGroup && (
            <Button
              type="button"
              size="sm"
              variant="outline"
              onClick={onCreateFleetGroup}
              disabled={loading}
              aria-label="Create new fleet group"
            >
              + New
            </Button>
          )}
        </div>
        <div className="text-[11px] font-mono text-fg-muted mt-1">
          Leave empty to add the node without a group — it'll land in the default scope.
        </div>
      </FormField>

      {isOutbound && (
        <FormField label="Agent dial address" variant="uppercase" required>
          <Input
            type="text"
            placeholder="vps.example.com:8443"
            value={dialAddress ?? ""}
            onChange={(e) => onDialAddressChange?.(e.target.value)}
            onBlur={() => setTouched((t) => ({ ...t, dialAddress: true }))}
            disabled={loading}
            aria-invalid={touched.dialAddress && !!dialError}
            aria-describedby={
              touched.dialAddress && dialError ? "enroll-dial-err" : undefined
            }
            className="font-mono text-xs"
          />
          {touched.dialAddress && dialError && (
            <div id="enroll-dial-err" className="text-xs text-status-error mt-1">
              {dialError}
            </div>
          )}
          <div className="text-[11px] font-mono text-fg-muted mt-1">
            The panel dials this from its egress to reach the agent. The agent will
            listen on the matching port locally.
          </div>
        </FormField>
      )}

      {!isOutbound && (
      <FormField label="Token lifetime" variant="uppercase">
        <fieldset className="flex flex-wrap gap-2 border-0 p-0 m-0">
          <legend className="sr-only">Token lifetime presets</legend>
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
        </fieldset>
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
      )}

      {/* PR-3c: collapse all the niche knobs (Telemt URLs, auth header,
          insecure-transport flag, install-script source) behind a
          single "Advanced" disclosure. Defaults work for the common
          single-host deploy; operators only open this when they need
          to deviate. */}
      {advancedOptions && onAdvancedOptionsChange && (
        <div className="flex flex-col gap-3">
          <button
            type="button"
            onClick={() => setAdvancedOpen((v) => !v)}
            aria-expanded={advancedOpen}
            className="self-start text-xs text-fg-muted hover:text-fg flex items-center gap-1"
          >
            <span aria-hidden="true">{advancedOpen ? "▾" : "▸"}</span>
            Advanced
          </button>
          {advancedOpen && (
            <div className="flex flex-col gap-3 pl-3 border-l border-divider">
              {showSourceToggle && (
                <FormField label="Install-script source" variant="uppercase">
                  <fieldset className="flex flex-wrap gap-2 border-0 p-0 m-0">
                    <legend className="sr-only">Install-script source toggle</legend>
                    {([
                      { value: "panel" as const, label: "Panel" },
                      { value: "github" as const, label: "GitHub" },
                    ]).map((opt) => {
                      const pressed = scriptSource === opt.value;
                      return (
                        <button
                          key={opt.value}
                          type="button"
                          aria-pressed={pressed}
                          disabled={loading}
                          onClick={() => onScriptSourceChange?.(opt.value)}
                          className={cn(
                            "px-3 py-1.5 rounded-xs text-xs transition-colors",
                            pressed
                              ? "bg-accent text-white"
                              : "border border-border text-fg-muted hover:text-fg",
                          )}
                        >
                          {opt.label}
                        </button>
                      );
                    })}
                  </fieldset>
                  <div className="text-[11px] text-fg-muted mt-1 leading-snug">
                    {scriptSource === "panel"
                      ? "Curl pulls install-agent.sh from <panel>/install-agent.sh."
                      : "Curl pulls install-agent.sh from raw.githubusercontent.com. Default for outbound (panel may be firewalled from the agent)."}
                  </div>
                </FormField>
              )}
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
                  loopback" guard. Warning tone so operators who don't
                  read the description still see this isn't the default. */}
              <label
                className="flex items-start gap-2 rounded-xs border border-status-warn/30 bg-status-warn/5 p-3 cursor-pointer"
                aria-label="Allow plaintext on public-IP / hostname panel"
              >
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
                    Allow plaintext on public-IP / hostname panel
                  </span>
                  <span className="text-[11px] text-fg-muted leading-snug">
                    Passes <code className="font-mono">--insecure-transport</code> so the
                    agent accepts an http:// panel URL on a public IP or a
                    hostname. Private IPs (10/8, 172.16/12, 192.168/16,
                    CGNAT, IPv6 ULA) are auto-trusted without this flag.
                    Bootstrap exchanges the agent private key in cleartext —
                    only tick on a trusted link.
                  </span>
                </span>
              </label>
            </div>
          )}
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
