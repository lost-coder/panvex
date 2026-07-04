// P5-T5: the "Apply" action for the Config tab.
//
// Wraps a @/ui Button + ConfirmDialog. If any of the changed paths maps
// to a restart-only field (requiresRestart), clicking opens a confirm
// dialog warning that Telemt will restart and briefly drop connections;
// the apply only proceeds on confirm. Hot-only changes apply
// immediately.
//
// Both apply paths (single-agent and group) are now ASYNC kickoffs: they
// return 202 and report per-agent progress elsewhere, so this button only
// gates the restart-confirm + kickoff and leaves outcome surfacing (progress
// indicator + completion toast) to the caller. The button is disabled while
// the kickoff request is in flight.

import { useState } from "react";
import { useTranslation } from "react-i18next";

import { Button, ConfirmDialog } from "@/ui";

import { requiresRestart } from "./fieldRegistry";

export interface ApplyConfigButtonProps {
  changedPaths: string[];
  onApply: () => Promise<void>;
  labelKey?: string;
  disabled?: boolean;
}

export function ApplyConfigButton({
  changedPaths,
  onApply,
  labelKey,
  disabled,
}: Readonly<ApplyConfigButtonProps>) {
  const { t } = useTranslation("servers");
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [inFlight, setInFlight] = useState(false);

  const needsRestart = requiresRestart(changedPaths);

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
    if (needsRestart) {
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
        title={t("config.apply.restartWarningTitle")}
        description={t("config.apply.restartWarning")}
        confirmLabel={t("config.apply.confirm")}
        cancelLabel={t("config.apply.cancel")}
        variant="danger"
        onConfirm={handleConfirm}
        onCancel={() => setConfirmOpen(false)}
      />
    </>
  );
}
