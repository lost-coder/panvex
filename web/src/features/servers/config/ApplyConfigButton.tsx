// P5-T5: the "Apply" action for the Config tab.
//
// Wraps a @/ui Button + ConfirmDialog. If any of the changed paths maps
// to a reload-only field (requiresReload), or the caller reports that
// the node's live config has drifted from the target (driftedFields),
// clicking opens a confirm dialog before the apply proceeds. Hot-only,
// non-drifted changes apply immediately.
//
// V4.2: drift-aware apply. The operator gets no warning today that
// applying overwrites a drifted node — this button now surfaces that,
// listing the diverging dotted paths, and lets the operator back out.
// When BOTH a reload change and a drift warning apply, they are combined
// into a SINGLE dialog rather than shown one after another — two stacked
// modals is the exact UX the drift warning is meant to avoid.
//
// Task 8 (Telemt Maestro reload): Telemt no longer restarts to pick up a
// reload-only field — Maestro reloads it in-process. The old
// "restart required" copy is gone; when a reload IS needed, the dialog
// instead asks the operator to choose a session policy: drain existing
// sessions (default, 30s window) or reload instantly and drop them. That
// choice becomes the `ApplyConfigRequest` body handed to `onApply` — a
// hot-only change (no reload, no drift) skips the dialog entirely and
// applies with an empty policy, which the panel defaults server-side.
//
// Both apply paths (single-agent and group) are now ASYNC kickoffs: they
// return 202 and report per-agent progress elsewhere, so this button only
// gates the confirm + kickoff and leaves outcome surfacing (progress
// indicator + completion toast) to the caller. The button is disabled while
// the kickoff request is in flight.

import { useId, useState } from "react";
import { useTranslation } from "react-i18next";

import { Button, ConfirmDialog } from "@/ui";
import type { ApplyConfigRequest } from "@/shared/api/schemas/requests/applyConfigRequest";

import { catalogEntry } from "./paramCatalog";
import { isContainerPath } from "@/features/servers/config/buildTree";

// Matches the panel's own default (see applyConfigRequest.ts / P5-T5
// config_apply_reload.go): an absent policy resolves to a 30s drain. The
// dialog's default choice mirrors that so "just click Apply" behaves the
// same whether or not the operator engages with the picker.
const DEFAULT_DRAIN_TIMEOUT_SECS = 30;

type ReloadChoice = "drain" | "instant";

export interface ApplyConfigButtonProps {
  changedPaths: string[];
  /**
   * Dotted paths where the node's live (observed) config has diverged from
   * the target. Per-agent only — callers with no single-agent drift value
   * (e.g. a fleet group) should pass `[]` or omit the prop; drift is never
   * invented at group scope.
   */
  driftedFields?: string[];
  onApply: (policy: ApplyConfigRequest) => Promise<void>;
  labelKey?: string;
  disabled?: boolean;
}

export function ApplyConfigButton({
  changedPaths,
  driftedFields,
  onApply,
  labelKey,
  disabled,
}: Readonly<ApplyConfigButtonProps>) {
  const { t } = useTranslation("servers");
  const drainId = useId();
  const instantId = useId();
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [inFlight, setInFlight] = useState(false);
  const [reloadChoice, setReloadChoice] = useState<ReloadChoice>("drain");

  // F8: контейнеры в каталоге не представлены (у них нет записей — ключи
  // задаёт оператор), но все три применяются только через перезагрузку
  // Maestro, поэтому классифицируются по пути, а не по записи каталога.
  const needsReload = changedPaths.some(
    (p) => catalogEntry(p)?.applyMode === "reload" || isContainerPath(p),
  );
  const drifted = driftedFields ?? [];
  const hasDrift = drifted.length > 0;
  const needsConfirm = needsReload || hasDrift;

  const dialogTitle = hasDrift
    ? t("config.apply.driftWarningTitle")
    : t("config.apply.sessionPolicy.title");
  const dialogDescription = [
    hasDrift
      ? t("config.apply.driftWarning", { fields: drifted.join(", ") })
      : null,
    needsReload ? t("config.apply.sessionPolicy.description") : null,
  ]
    .filter(Boolean)
    .join(" ");

  function reloadPolicy(): ApplyConfigRequest {
    if (!needsReload) return {};
    return reloadChoice === "drain"
      ? { reload_mode: "drain", reload_timeout_secs: DEFAULT_DRAIN_TIMEOUT_SECS }
      : { reload_mode: "instant" };
  }

  async function runApply(policy: ApplyConfigRequest) {
    setInFlight(true);
    try {
      // Both paths (agent + group) are async kickoffs: the caller renders the
      // outcome (progress + toast) from the batch status.
      await onApply(policy);
    } finally {
      setInFlight(false);
    }
  }

  function handleClick() {
    if (needsConfirm) {
      setReloadChoice("drain");
      setConfirmOpen(true);
      return;
    }
    void runApply({});
  }

  function handleConfirm() {
    setConfirmOpen(false);
    void runApply(reloadPolicy());
  }

  return (
    <>
      <Button
        onClick={handleClick}
        disabled={disabled || inFlight}
      >
        {t(labelKey ?? "config.apply.button")}
      </Button>
      <ConfirmDialog
        open={confirmOpen}
        title={dialogTitle}
        description={dialogDescription}
        confirmLabel={t("config.apply.confirm")}
        cancelLabel={t("config.apply.cancel")}
        variant="danger"
        onConfirm={handleConfirm}
        onCancel={() => setConfirmOpen(false)}
      >
        {needsReload && (
          <fieldset className="flex flex-col gap-2 border-0 p-0 m-0">
            <legend className="sr-only">
              {t("config.apply.sessionPolicy.title")}
            </legend>
            <div className="flex items-start gap-2 text-sm text-fg">
              <input
                id={drainId}
                type="radio"
                name="reload-session-policy"
                value="drain"
                checked={reloadChoice === "drain"}
                onChange={() => setReloadChoice("drain")}
                className="mt-0.5"
              />
              <label htmlFor={drainId} className="flex flex-col cursor-pointer">
                <span className="font-medium">
                  {t("config.apply.sessionPolicy.drainLabel")}
                </span>
                <span className="text-micro text-fg-muted">
                  {t("config.apply.sessionPolicy.drainHint")}
                </span>
              </label>
            </div>
            <div className="flex items-start gap-2 text-sm text-fg">
              <input
                id={instantId}
                type="radio"
                name="reload-session-policy"
                value="instant"
                checked={reloadChoice === "instant"}
                onChange={() => setReloadChoice("instant")}
                className="mt-0.5"
              />
              <label htmlFor={instantId} className="flex flex-col cursor-pointer">
                <span className="font-medium">
                  {t("config.apply.sessionPolicy.instantLabel")}
                </span>
                <span className="text-micro text-fg-muted">
                  {t("config.apply.sessionPolicy.instantHint")}
                </span>
              </label>
            </div>
          </fieldset>
        )}
      </ConfirmDialog>
    </>
  );
}
