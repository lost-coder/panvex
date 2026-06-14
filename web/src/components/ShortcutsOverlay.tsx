import { useEffect, useRef } from "react";
import { X } from "lucide-react";
import { useTranslation } from "react-i18next";

import { META_SHORTCUTS, NAV_SHORTCUTS } from "@/app/shortcuts";
import { useKeyboardShortcut } from "@/shared/hooks";

interface ShortcutsOverlayProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

/**
 * UX-13: help overlay enumerating the keyboard shortcuts. Controlled by
 * ProtectedShell so the sidebar "?" hint can open it too. The list is
 * derived from src/app/shortcuts.ts — single registry, no drift.
 */
export function ShortcutsOverlay({ open, onOpenChange }: Readonly<ShortcutsOverlayProps>) {
  const { t } = useTranslation("ui");
  const dialogRef = useRef<HTMLDialogElement>(null);

  useKeyboardShortcut("?", () => onOpenChange(!open));
  useKeyboardShortcut("Escape", () => onOpenChange(false), { enabled: open });

  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    if (open && !dialog.open) dialog.showModal();
    if (!open && dialog.open) dialog.close();
  }, [open]);

  const items = [...NAV_SHORTCUTS, ...META_SHORTCUTS];

  return (
    <dialog
      ref={dialogRef}
      aria-label={t("shortcuts.title")}
      onClose={() => onOpenChange(false)}
      className="fixed inset-0 m-auto max-h-fit max-w-fit p-0 bg-transparent backdrop:bg-black/60"
    >
      <div className="relative w-full max-w-sm rounded-xl border border-border bg-bg-card p-5 shadow-xl">
        <button
          type="button"
          aria-label={t("shortcuts.close")}
          onClick={() => onOpenChange(false)}
          className="absolute top-2 right-2 rounded-xs p-1 text-fg-muted hover:text-fg hover:bg-bg-hover transition-colors"
        >
          <X size={16} aria-hidden="true" />
        </button>
        <h2 className="text-sm font-semibold text-fg mb-3">{t("shortcuts.title")}</h2>
        <ul className="flex flex-col gap-2 text-xs">
          {items.map((item) => (
            <li key={item.keys} className="flex items-center justify-between gap-4">
              <span className="text-fg-muted">{t(item.i18nKey)}</span>
              <kbd className="rounded border border-border bg-bg px-2 py-0.5 font-mono text-nano uppercase tracking-wide text-fg">
                {item.keys}
              </kbd>
            </li>
          ))}
        </ul>
        <p className="mt-4 text-micro text-fg-muted">{t("shortcuts.inputsNote")}</p>
      </div>
    </dialog>
  );
}
