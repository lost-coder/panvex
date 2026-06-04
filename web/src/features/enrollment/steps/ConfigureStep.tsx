import { useState } from "react";
import { Trans, useTranslation } from "react-i18next";

import { cn } from "@/ui/lib/cn";
import { Button } from "@/ui/base/button";
import { FormField } from "@/ui/base/form-field";
import { Input } from "@/ui/base/input";
import { Select } from "@/ui/base/select";
import type { EnrollmentWizardProps } from "@/shared/api/types-pages/pages";

export function ConfigureStep(props: Readonly<EnrollmentWizardProps>) {
  const { t } = useTranslation("enrollment");

  const TTL_PRESETS = [
    { value: 3600, label: t("configure.ttl.preset1h") },
    { value: 21600, label: t("configure.ttl.preset6h") },
    { value: 86400, label: t("configure.ttl.preset24h") },
  ];

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

  const nodeNameError = nodeName.trim() ? undefined : t("configure.nodeName.required");
  const ttlError = tokenTtl <= 0 ? t("configure.ttl.invalid") : undefined;
  // Outbound requires a host:port the panel can dial. We don't validate
  // the full RFC here — the server enforces `net.SplitHostPort` — but a
  // visible colon catches the most common typo before the round-trip.
  const dialError = isOutbound
    ? !dialAddress || !dialAddress.trim()
      ? t("configure.dialAddress.required")
      : !/^\S+:\d+$/.test(dialAddress.trim())
        ? t("configure.dialAddress.invalid")
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
        <FormField label={t("configure.mode.label")} variant="uppercase">
          <div
            role="radiogroup"
            aria-label={t("configure.mode.groupLabel")}
            className="inline-flex rounded-xs border border-border p-0.5 bg-bg w-full"
          >
            {([
              { value: "inbound", label: t("configure.mode.inbound"), aria: t("configure.mode.inboundAria") },
              { value: "outbound", label: t("configure.mode.outbound"), aria: t("configure.mode.outboundAria") },
            ] as const).map((opt) => {
              const selected = effectiveMode === opt.value;
              return (
                <button
                  key={opt.value}
                  type="button"
                  role="radio"
                  aria-checked={selected}
                  aria-label={opt.aria}
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
          <div className="text-micro text-fg-muted mt-1 leading-snug">
            {effectiveMode === "inbound"
              ? t("configure.mode.inboundHint")
              : t("configure.mode.outboundHint")}
          </div>
        </FormField>
      )}

      <FormField label={t("configure.nodeName.label")} variant="uppercase" required>
        <Input
          type="text"
          placeholder={t("configure.nodeName.placeholder")}
          value={nodeName}
          onChange={(e) => onNodeNameChange(e.target.value)}
          onBlur={() => setTouched((tt) => ({ ...tt, nodeName: true }))}
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
      <FormField label={t("configure.fleetGroup.label")} variant="uppercase">
        <div className="flex gap-2">
          <Select
            className="flex-1"
            value={selectedFleetGroup}
            options={[
              { value: "", label: t("configure.fleetGroup.none") },
              ...fleetGroups.map((g) => ({
                value: g.id,
                label: t("configure.fleetGroup.option", {
                  name: g.name ?? g.label ?? g.id,
                  count: g.nodeCount ?? g.agentCount ?? 0,
                }),
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
              aria-label={t("configure.fleetGroup.createAria")}
            >
              {t("configure.fleetGroup.new")}
            </Button>
          )}
        </div>
        <div className="text-micro font-mono text-fg-muted mt-1">
          {t("configure.fleetGroup.hint")}
        </div>
      </FormField>

      {isOutbound && (
        <FormField label={t("configure.dialAddress.label")} variant="uppercase" required>
          <Input
            type="text"
            placeholder={t("configure.dialAddress.placeholder")}
            value={dialAddress ?? ""}
            onChange={(e) => onDialAddressChange?.(e.target.value)}
            onBlur={() => setTouched((tt) => ({ ...tt, dialAddress: true }))}
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
          <div className="text-micro font-mono text-fg-muted mt-1">
            {t("configure.dialAddress.hint")}
          </div>
        </FormField>
      )}

      {!isOutbound && (
      <FormField label={t("configure.ttl.label")} variant="uppercase">
        <fieldset className="flex flex-wrap gap-2 border-0 p-0 m-0">
          <legend className="sr-only">{t("configure.ttl.legend")}</legend>
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
                  setTouched((tt) => ({ ...tt, ttl: true }));
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
              setTouched((tt) => ({ ...tt, ttl: true }));
            }}
            className={cn(
              "px-3 py-1.5 rounded-xs text-xs transition-colors",
              customTtl
                ? "bg-accent text-white"
                : "border border-border text-fg-muted hover:text-fg",
            )}
          >
            {t("configure.ttl.custom")}
          </button>
        </fieldset>
        {customTtl && (
          <Input
            type="number"
            min={1}
            placeholder={t("configure.ttl.customPlaceholder")}
            value={tokenTtl}
            onChange={(e) => onTokenTtlChange(Number(e.target.value))}
            onBlur={() => setTouched((tt) => ({ ...tt, ttl: true }))}
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
            {t("configure.advanced.toggle")}
          </button>
          {advancedOpen && (
            <div className="flex flex-col gap-3 pl-3 border-l border-divider">
              {showSourceToggle && (
                <FormField label={t("configure.advanced.scriptSource.label")} variant="uppercase">
                  <fieldset className="flex flex-wrap gap-2 border-0 p-0 m-0">
                    <legend className="sr-only">{t("configure.advanced.scriptSource.legend")}</legend>
                    {([
                      { value: "panel" as const, label: t("configure.advanced.scriptSource.panel") },
                      { value: "github" as const, label: t("configure.advanced.scriptSource.github") },
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
                  <div className="text-micro text-fg-muted mt-1 leading-snug">
                    {scriptSource === "panel"
                      ? t("configure.advanced.scriptSource.panelHint")
                      : t("configure.advanced.scriptSource.githubHint")}
                  </div>
                </FormField>
              )}
              <FormField label={t("configure.advanced.telemtUrl")} variant="uppercase">
                <Input
                  value={advancedOptions.telemtUrl}
                  onChange={(e) =>
                    onAdvancedOptionsChange({ ...advancedOptions, telemtUrl: e.target.value })
                  }
                  className="font-mono text-xs"
                />
              </FormField>
              <FormField label={t("configure.advanced.telemtMetricsUrl")} variant="uppercase">
                <Input
                  value={advancedOptions.telemtMetricsUrl}
                  onChange={(e) =>
                    onAdvancedOptionsChange({
                      ...advancedOptions,
                      telemtMetricsUrl: e.target.value,
                    })
                  }
                  placeholder={t("configure.advanced.telemtMetricsPlaceholder")}
                  className="font-mono text-xs"
                />
              </FormField>
              <FormField label={t("configure.advanced.telemtAuth")} variant="uppercase">
                <Input
                  value={advancedOptions.telemtAuth}
                  onChange={(e) =>
                    onAdvancedOptionsChange({ ...advancedOptions, telemtAuth: e.target.value })
                  }
                  placeholder={t("configure.advanced.telemtAuthPlaceholder")}
                  className="font-mono text-xs"
                />
              </FormField>
              {/* Opt-in relaxation of the agent's "https required unless
                  loopback" guard. Warning tone so operators who don't
                  read the description still see this isn't the default. */}
              <label
                className="flex items-start gap-2 rounded-xs border border-status-warn/30 bg-status-warn/5 p-3 cursor-pointer"
                aria-label={t("configure.advanced.insecure.aria")}
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
                    {t("configure.advanced.insecure.title")}
                  </span>
                  <span className="text-micro text-fg-muted leading-snug">
                    <Trans
                      i18nKey="configure.advanced.insecure.description"
                      ns="enrollment"
                      components={[<code key="0" className="font-mono" />]}
                    />
                  </span>
                </span>
              </label>
            </div>
          )}
        </div>
      )}

      <div className="rounded-xs bg-accent/8 border border-accent/20 p-3 text-xs text-accent">
        <strong>{t("configure.telemtNote.label")}</strong> {t("configure.telemtNote.text")}
      </div>

      {error && <div className="text-xs text-status-error">{error}</div>}

      <Button onClick={handleGenerate} disabled={loading}>
        {loading ? t("configure.generating") : t("configure.generate")}
      </Button>
    </div>
  );
}
