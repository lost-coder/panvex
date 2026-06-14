// P5-T5: the "Apply" action for the Config tab.
//
// Wraps a @/ui Button + ConfirmDialog. If any of the changed paths maps
// to a restart-only field (requiresRestart), clicking opens a confirm
// dialog warning that Telemt will restart and briefly drop connections;
// the apply only proceeds on confirm. Hot-only changes apply
// immediately. The ApplyResult is surfaced through the global toast:
// a non-empty error/failed → toast.error, otherwise toast.success with
// the applied count. The button is disabled while a request is in flight.

import { useState } from "react";
import { useTranslation } from "react-i18next";

import { useToast } from "@/app/providers/ToastProvider";
import type { ApplyResult } from "@/shared/api/schemas/config";
import { Button, ConfirmDialog } from "@/ui";

import { requiresRestart } from "./fieldRegistry";

export interface ApplyConfigButtonProps {
  changedPaths: string[];
  onApply: () => Promise<ApplyResult>;
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
