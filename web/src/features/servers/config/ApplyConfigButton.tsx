// P5-T5: the "Apply" action for the Config tab.
//
// Wraps a @/ui Button + ConfirmDialog. If any of the changed paths maps
// to a restart-only field (requiresRestart), clicking opens a confirm
// dialog warning that Telemt will restart and briefly drop connections;
// the apply only proceeds on confirm. Hot-only changes apply
// immediately.
//
// onApply may resolve with an ApplyResult (the SYNCHRONOUS single-agent
// path) — in which case a non-empty error/failed toasts an error and a
// clean result toasts success with the applied count. Or it may resolve
// with void (the ASYNC group-apply path, which returns 202 and reports
// per-agent progress elsewhere) — in which case this button only gates the
// restart-confirm + kickoff and leaves outcome surfacing to the caller.
// The button is disabled while the kickoff request is in flight.

import { useState } from "react";
import { useTranslation } from "react-i18next";

import { useToast } from "@/app/providers/ToastProvider";
import type { ApplyResult } from "@/shared/api/schemas/config";
import { Button, ConfirmDialog } from "@/ui";

import { requiresRestart } from "./fieldRegistry";

export interface ApplyConfigButtonProps {
  changedPaths: string[];
  onApply: () => Promise<ApplyResult | void>;
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
  const toast = useToast();
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [inFlight, setInFlight] = useState(false);

  const needsRestart = requiresRestart(changedPaths);

  async function runApply() {
    setInFlight(true);
    try {
      const result = await onApply();
      // Async kickoff (group apply) resolves with void — the caller owns
      // progress/outcome surfacing, so there is nothing to toast here.
      if (!result) {
        return;
      }
      if (result.error !== "" || result.failed !== "") {
        toast.error(
          t("config.apply.failed", { agent: result.failed, error: result.error }),
        );
      } else {
        toast.success(t("config.apply.applied", { count: result.applied }));
      }
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
