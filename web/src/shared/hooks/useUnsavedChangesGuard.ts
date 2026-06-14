import { useBlocker } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

import { useConfirm } from "@/app/providers/ConfirmProvider";

/**
 * Audit E4: guards dirty forms. In-app navigation is intercepted via
 * TanStack Router's useBlocker and routed through the shared
 * ConfirmDialog; tab close / reload is covered by enableBeforeUnload
 * (the native browser prompt — custom UI is impossible there by spec).
 *
 * Returning true from shouldBlockFn KEEPS the user on the page.
 */
export function useUnsavedChangesGuard(dirty: boolean): void {
  const confirm = useConfirm();
  const { t } = useTranslation("common");

  useBlocker({
    shouldBlockFn: async () => {
      if (!dirty) return false;
      const leave = await confirm({
        title: t("unsaved.title"),
        body: t("unsaved.body"),
        confirmLabel: t("unsaved.leave"),
        cancelLabel: t("unsaved.stay"),
        variant: "danger",
      });
      return !leave;
    },
    disabled: !dirty,
    enableBeforeUnload: () => dirty,
  });
}
