import { useEffect, useState } from "react";

import { useKeyboardShortcut } from "@/shared/hooks";

interface ShortcutItem {
  keys: string;
  description: string;
}

const SHORTCUTS: ShortcutItem[] = [
  { keys: "g d", description: "Перейти на Dashboard" },
  { keys: "g s", description: "Перейти на Servers" },
  { keys: "g c", description: "Перейти на Clients" },
  { keys: "g t", description: "Перейти на Settings" },
  { keys: "?", description: "Показать список сочетаний" },
  { keys: "Esc", description: "Закрыть этот диалог" },
];

/**
 * UX-13: help overlay that enumerates the keyboard shortcuts wired into
 * the app. Rendered once inside ProtectedShell; `?` toggles it. A
 * lightweight modal without focus-trap because the list is read-only
 * and `Esc` dismisses — the full <Dialog> primitive would be overkill.
 */
export function ShortcutsOverlay() {
  const [open, setOpen] = useState(false);

  useKeyboardShortcut("?", () => setOpen((prev) => !prev));
  useKeyboardShortcut("Escape", () => setOpen(false), { enabled: open });

  useEffect(() => {
    if (!open) return;
    const previous = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.body.style.overflow = previous;
    };
  }, [open]);

  if (!open) return null;

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label="Сочетания клавиш"
      className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/60"
      onClick={() => setOpen(false)}
    >
      <div
        className="w-full max-w-sm rounded-xl border border-border bg-bg-card p-5 shadow-xl"
        onClick={(event) => event.stopPropagation()}
      >
        <h2 className="text-sm font-semibold text-fg mb-3">Сочетания клавиш</h2>
        <ul className="flex flex-col gap-2 text-xs">
          {SHORTCUTS.map((item) => (
            <li key={item.keys} className="flex items-center justify-between gap-4">
              <span className="text-fg-muted">{item.description}</span>
              <kbd className="rounded border border-border bg-bg px-2 py-0.5 font-mono text-[10px] uppercase tracking-wide text-fg">
                {item.keys}
              </kbd>
            </li>
          ))}
        </ul>
        <p className="mt-4 text-[11px] text-fg-muted">
          Сочетания не срабатывают внутри полей ввода.
        </p>
      </div>
    </div>
  );
}
