// P5-T5: the "Apply" action for the Config tab.
//
// Wraps a @/ui Button + ConfirmDialog. If any of the changed paths maps
// to a restart-only field (requiresRestart), or the caller reports that
// the node's live config has drifted from the target (driftedFields),
// clicking opens a confirm dialog before the apply proceeds. Hot-only,
// non-drifted changes apply immediately.
//
// V4.2: drift-aware apply. The operator gets no warning today that
// applying overwrites a drifted node — this button now surfaces that,
// listing the diverging dotted paths, and lets the operator back out.
// When BOTH a restart warning and a drift warning apply, they are
// combined into a SINGLE dialog rather than shown one after another —
// two stacked modals is the exact UX the drift warning is meant to avoid.
//
// Both apply paths (single-agent and group) are now ASYNC kickoffs: they
// return 202 and report per-agent progress elsewhere, so this button only
// gates the confirm + kickoff and leaves outcome surfacing (progress
// indicator + completion toast) to the caller. The button is disabled while
// the kickoff request is in flight.

import { useState } from "react";
import { useTranslation } from "react-i18next";

import { Button, ConfirmDialog } from "@/ui";

import { requiresRestart } from "./fieldRegistry";

export interface ApplyConfigButtonProps {
  changedPaths: string[];
  /**
   * Dotted paths where the node's live (observed) config has diverged from
   * the target. Per-agent only — callers with no single-agent drift value
   * (e.g. a fleet group) should pass `[]` or omit the prop; drift is never
   * invented at group scope.
   */
  driftedFields?: string[];
  onApply: () => Promise<void>;
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
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [inFlight, setInFlight] = useState(false);

  const needsRestart = requiresRestart(changedPaths);
  const drifted = driftedFields ?? [];
  const hasDrift = drifted.length > 0;
  const needsConfirm = needsRestart || hasDrift;

  const dialogTitle = hasDrift
    ? t("config.apply.driftWarningTitle")
    : t("config.apply.restartWarningTitle");
  const dialogDescription = [
    hasDrift
      ? t("config.apply.driftWarning", { fields: drifted.join(", ") })
      : null,
    needsRestart ? t("config.apply.restartWarning") : null,
  ]
    .filter(Boolean)
    .join(" ");

  async function runApply() {
    setInFlight(true);
    try {
      // Both paths (agent + group) are async kickoffs: the caller renders the
      // outcome (progress + toast) from the batch status.
      await onApply();
    } finally {
      setInFlight(false);
    }
  }

  function handleClick() {
    if (needsConfirm) {
      setConfirmOpen(true);
      return;
    }
    void runApply();
  }

  function handleConfirm() {
    setConfirmOpen(false);
    void runApply();
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
      />
    </>
  );
}
