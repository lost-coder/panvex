// Task 13: "Обновление Telemt" section on the server-detail page. A
// self-contained slot (mirrors EnrollmentHistory / RuntimeEvents) — the
// container wires it in with just `agentId` so the presentational
// ServerDetailPage stays free of QueryClient dependencies in unit tests.
//
// Consumes GET/PUT /api/agents/{id}/telemt/update-strategy (Task 9): the
// GET response carries both the operator-persisted strategy (null when
// never configured) and the agent's live-detected probe (null until the
// agent has reported one) in a single round trip.

import { useEffect, useId, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import { Badge, Button, FormField, Input, Select, Spinner, Toggle, Tooltip } from "@/ui";
import { useConfirm } from "@/app/providers/ConfirmProvider";
import { useToast } from "@/app/providers/ToastProvider";
import type { TelemtUpdateProbe, TelemtUpdateStrategy } from "@/shared/api/api";
import { telemtUpdateStrategySchema } from "@/shared/api/schemas";

import { Fold } from "./components/Fold";
import { semverLt } from "./semver";
import {
  composeRestartSpec,
  isValidRestartName,
  parseRestartSpec,
  type RestartPreset,
  type RestartSpecForm,
} from "./telemtRestartSpec";
import {
  useDispatchTelemtUpdate,
  useTelemtUpdateStrategy,
  usePutTelemtUpdateStrategy,
} from "./useTelemtUpdateStrategy";

interface Props {
  agentId: string;
  /** Currently running Telemt version on this node (systemInfo.version). */
  currentVersion: string;
  /** Panel's cached latest known Telemt release, "" when never checked. */
  latestVersion?: string | undefined;
}

// Client-side mirror of handleDispatchTelemtUpdate's guard chain (see
// http_telemt_update.go) for the three conditions the dashboard can know
// about *before* attempting the POST: no strategy configured, strategy mode
// isn't "binary", or the agent's live probe reports it can't do an in-place
// update. `no_known_release` is NOT included here — the client doesn't try
// to predict whether the panel has a cached latest version; that 409 (if it
// happens) is caught and toasted after the POST like any other guard.
type DisableReason = "strategy_not_configured" | "mode_not_binary" | "update_unavailable";

function disableReason(
  strategy: TelemtUpdateStrategy | null,
  probe: TelemtUpdateProbe | null,
): DisableReason | null {
  if (!strategy) return "strategy_not_configured";
  if (strategy.mode !== "binary") return "mode_not_binary";
  if (!probe || !probe.available) return "update_unavailable";
  return null;
}

type Mode = TelemtUpdateStrategy["mode"];

interface FormState extends RestartSpecForm {
  mode: Mode;
  binaryPath: string;
  flavorV3: boolean;
}

const PRESETS: readonly RestartPreset[] = ["systemd", "procd", "openrc", "runit", "custom"];

function formFromStrategy(strategy: TelemtUpdateStrategy | null): FormState {
  if (!strategy) {
    return { mode: "none", preset: "systemd", name: "", command: "", binaryPath: "", flavorV3: false };
  }
  return {
    mode: strategy.mode,
    ...parseRestartSpec(strategy.restart_spec),
    binaryPath: strategy.binary_path,
    flavorV3: strategy.asset_flavor === "v3",
  };
}

function formFromProbe(probe: TelemtUpdateProbe, prev: FormState): FormState {
  const mode: Mode = probe.mode === "binary" || probe.mode === "docker" ? probe.mode : "none";
  return {
    ...prev,
    mode,
    ...parseRestartSpec(probe.suggested_restart_spec),
    binaryPath: probe.binary_path || prev.binaryPath,
  };
}

function toStrategyPayload(form: FormState): TelemtUpdateStrategy {
  return {
    mode: form.mode,
    restart_spec: composeRestartSpec(form),
    binary_path: form.binaryPath.trim(),
    asset_flavor: form.flavorV3 ? "v3" : "",
  };
}

export function TelemtUpdateSection({ agentId, currentVersion, latestVersion }: Readonly<Props>) {
  const { t } = useTranslation("servers");
  const toast = useToast();
  const confirm = useConfirm();
  const query = useTelemtUpdateStrategy(agentId);
  const putMutation = usePutTelemtUpdateStrategy(agentId);
  const dispatchMutation = useDispatchTelemtUpdate(agentId);

  const [form, setForm] = useState<FormState>(() => formFromStrategy(null));
  const [validationError, setValidationError] = useState<string | null>(null);
  const seededRef = useRef(false);

  // Seed the form once from the first successful load — a subsequent
  // background refetch (e.g. cache invalidation from an unrelated save
  // elsewhere) must not clobber an in-progress edit.
  useEffect(() => {
    if (seededRef.current || !query.data) return;
    setForm(formFromStrategy(query.data.strategy));
    seededRef.current = true;
  }, [query.data]);

  const modeId = useId();
  const presetId = useId();
  const nameId = useId();
  const commandId = useId();
  const binaryPathId = useId();
  const flavorId = useId();

  function update<K extends keyof FormState>(key: K, value: FormState[K]) {
    setValidationError(null);
    setForm((prev) => ({ ...prev, [key]: value }));
  }

  function applySuggested(probe: TelemtUpdateProbe) {
    setValidationError(null);
    setForm((prev) => formFromProbe(probe, prev));
  }

  function validate(): string | null {
    if (form.mode === "binary") {
      if (form.preset === "custom") {
        if (!form.command.trim()) return t("telemtUpdate.form.validation.commandRequired");
      } else if (!isValidRestartName(form.name)) {
        return form.name.trim()
          ? t("telemtUpdate.form.validation.unitNameInvalid")
          : t("telemtUpdate.form.validation.unitNameRequired");
      }
      const binaryPath = form.binaryPath.trim();
      if (!binaryPath) return t("telemtUpdate.form.validation.binaryPathRequired");
      // Mirrors the server's filepath.IsAbs check — catch a relative path
      // client-side instead of round-tripping to a 400.
      if (!binaryPath.startsWith("/")) return t("telemtUpdate.form.validation.binaryPathNotAbsolute");
    }
    const payload = toStrategyPayload(form);
    const result = telemtUpdateStrategySchema.safeParse(payload);
    if (!result.success) return t("telemtUpdate.form.validation.invalid");
    return null;
  }

  function handleSave() {
    const err = validate();
    if (err) {
      setValidationError(err);
      return;
    }
    setValidationError(null);
    putMutation.mutate(toStrategyPayload(form), {
      onSuccess: () => toast.success(t("telemtUpdate.form.saved")),
    });
  }

  const probe = query.data?.probe ?? null;
  const strategy = query.data?.strategy ?? null;
  const updateAvailable = semverLt(currentVersion, latestVersion ?? "");
  const reason = disableReason(strategy, probe);

  async function handleUpdateClick() {
    const version = latestVersion ?? "";
    const ok = await confirm({
      title: t("telemtUpdate.action.confirmTitle"),
      body: t("telemtUpdate.action.confirmBody", { version }),
      confirmLabel: t("telemtUpdate.action.confirm"),
    });
    if (!ok) return;
    dispatchMutation.mutate(version);
  }

  const updateButton = (
    <Button size="sm" disabled={!!reason || dispatchMutation.isPending} onClick={handleUpdateClick}>
      {dispatchMutation.isPending
        ? t("telemtUpdate.action.updating")
        : t("telemtUpdate.action.button", { version: latestVersion ?? "" })}
    </Button>
  );

  return (
    <Fold
      title={t("telemtUpdate.title")}
      defaultOpen={false}
      rightHint={
        updateAvailable ? (
          <Badge variant="accent">
            {t("telemtUpdate.action.badge", { current: currentVersion, latest: latestVersion })}
          </Badge>
        ) : undefined
      }
    >
      {query.isLoading ? (
        <div className="flex items-center gap-2 text-sm text-fg-muted" aria-busy aria-live="polite">
          <Spinner />
          {t("telemtUpdate.loading")}
        </div>
      ) : query.isError ? (
        <div className="text-sm text-status-error">{t("telemtUpdate.error")}</div>
      ) : (
        <div className="flex flex-col gap-4">
          {updateAvailable && (
            <div className="flex flex-col gap-2 rounded-md border border-divider bg-bg-card/50 p-3">
              {strategy?.mode === "docker" ? (
                <>
                  <span className="text-sm text-fg">{t("telemtUpdate.action.dockerHint")}</span>
                  <code className="rounded bg-bg-inset px-2 py-1 font-mono text-xs text-fg">
                    {t("telemtUpdate.action.dockerCommand")}
                  </code>
                  <span className="text-xs text-fg-muted">{t("telemtUpdate.action.dockerHintFooter")}</span>
                </>
              ) : (
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <span className="text-sm text-fg">
                    {t("telemtUpdate.action.available", { version: latestVersion })}
                  </span>
                  {reason ? (
                    <Tooltip content={t(`telemtUpdate.action.disabledReason.${reason}`)}>
                      <span className="inline-flex">{updateButton}</span>
                    </Tooltip>
                  ) : (
                    updateButton
                  )}
                </div>
              )}
            </div>
          )}

          {probe && (
            <div className="flex flex-col gap-2 rounded-md border border-divider bg-bg-card/50 p-3">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <span className="text-xs font-semibold uppercase tracking-wider text-fg-muted">
                  {t("telemtUpdate.probe.title")}
                </span>
                <Button variant="outline" size="sm" onClick={() => applySuggested(probe)}>
                  {t("telemtUpdate.probe.apply")}
                </Button>
              </div>
              <div className="flex flex-col gap-1 text-sm text-fg">
                <span>
                  {t("telemtUpdate.probe.mode", {
                    mode: t(`telemtUpdate.form.mode.${probe.mode}`, { defaultValue: probe.mode }),
                  })}
                </span>
                {probe.suggested_restart_spec && (
                  <span className="font-mono text-xs text-fg-muted">
                    {t("telemtUpdate.probe.restartSpec", { spec: probe.suggested_restart_spec })}
                  </span>
                )}
                {probe.binary_path && (
                  <span className="font-mono text-xs text-fg-muted">
                    {t("telemtUpdate.probe.binaryPath", { path: probe.binary_path })}
                  </span>
                )}
              </div>
              {!probe.available && (
                <div className="text-xs text-status-warn">
                  {t("telemtUpdate.probe.unavailable")}
                  {probe.reason
                    ? `: ${t(`telemtUpdate.reason.${probe.reason}`, { defaultValue: probe.reason })}`
                    : null}
                </div>
              )}
            </div>
          )}

          <FormField label={t("telemtUpdate.form.modeLabel")} variant="uppercase" htmlFor={modeId}>
            <Select
              id={modeId}
              value={form.mode}
              onChange={(v) => update("mode", v as Mode)}
              options={(["binary", "docker", "none"] as const).map((m) => ({
                value: m,
                label: t(`telemtUpdate.form.mode.${m}`),
              }))}
            />
          </FormField>

          {form.mode === "binary" && (
            <>
              <FormField label={t("telemtUpdate.form.presetLabel")} variant="uppercase" htmlFor={presetId}>
                <Select
                  id={presetId}
                  value={form.preset}
                  onChange={(v) => update("preset", v as RestartPreset)}
                  options={PRESETS.map((p) => ({ value: p, label: t(`telemtUpdate.form.preset.${p}`) }))}
                />
              </FormField>

              {form.preset === "custom" ? (
                <FormField
                  label={t("telemtUpdate.form.customCommandLabel")}
                  variant="uppercase"
                  htmlFor={commandId}
                >
                  <Input
                    id={commandId}
                    type="text"
                    value={form.command}
                    onChange={(e) => update("command", e.target.value)}
                    placeholder={t("telemtUpdate.form.customCommandPlaceholder")}
                    autoComplete="off"
                  />
                </FormField>
              ) : (
                <FormField label={t("telemtUpdate.form.unitNameLabel")} variant="uppercase" htmlFor={nameId}>
                  <Input
                    id={nameId}
                    type="text"
                    value={form.name}
                    onChange={(e) => update("name", e.target.value)}
                    placeholder={t("telemtUpdate.form.unitNamePlaceholder")}
                    autoComplete="off"
                  />
                </FormField>
              )}

              <FormField
                label={t("telemtUpdate.form.binaryPathLabel")}
                variant="uppercase"
                htmlFor={binaryPathId}
              >
                <Input
                  id={binaryPathId}
                  type="text"
                  value={form.binaryPath}
                  onChange={(e) => update("binaryPath", e.target.value)}
                  placeholder={t("telemtUpdate.form.binaryPathPlaceholder")}
                  autoComplete="off"
                />
              </FormField>

              <div className="flex items-center gap-2">
                <Toggle id={flavorId} checked={form.flavorV3} onChange={(v) => update("flavorV3", v)} />
                <label htmlFor={flavorId} className="flex flex-col cursor-pointer text-sm text-fg">
                  <span>{t("telemtUpdate.form.flavorV3Label")}</span>
                  <span className="text-caption text-fg-muted">{t("telemtUpdate.form.flavorV3Hint")}</span>
                </label>
              </div>
            </>
          )}

          {validationError && <div className="text-xs text-status-error">{validationError}</div>}

          <div className="flex justify-end">
            <Button onClick={handleSave} disabled={putMutation.isPending}>
              {putMutation.isPending ? t("telemtUpdate.form.saving") : t("telemtUpdate.form.save")}
            </Button>
          </div>
        </div>
      )}
    </Fold>
  );
}
