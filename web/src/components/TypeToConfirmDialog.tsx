import { useEffect, useId, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import { Button } from "@/ui/base/button";
import { Input } from "@/ui/base/input";
import { cn } from "@/ui/lib/cn";

export interface TypeToConfirmDialogProps {
  open: boolean;
  title: string;
  description: string;
  /** The exact string the operator must type before confirm enables. */
  requireTypeMatch: string;
  confirmLabel?: string | undefined;
  cancelLabel?: string | undefined;
  variant?: "default" | "danger" | undefined;
  onConfirm: () => void;
  onCancel: () => void;
}

/**
 * UX-05: "type name to confirm" variant of ConfirmDialog. Forces the
 * operator to type the exact label (usually the resource name) before
 * the confirm button enables — deliberately slow so a stray keystroke
 * cannot delete a production server or revoke a user.
 */
export function TypeToConfirmDialog({
  open,
  title,
  description,
  requireTypeMatch,
  confirmLabel = "Confirm",
  cancelLabel = "Cancel",
  variant = "default",
  onConfirm,
  onCancel,
}: Readonly<TypeToConfirmDialogProps>) {
  const dialogRef = useRef<HTMLDialogElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const titleId = useId();
  const descId = useId();
  const inputId = useId();
  const { t } = useTranslation("common");
  const [typed, setTyped] = useState("");

  useEffect(() => {
    const el = dialogRef.current;
    if (!el) return;
    if (open && !el.open) {
      el.showModal();
      setTyped("");
      // Replaces the autoFocus attribute (jsx-a11y/no-autofocus) — we
      // still need initial focus on the confirm-text input so the
      // operator can start typing immediately, but moving it into a
      // post-mount effect keeps the rule happy.
      inputRef.current?.focus();
    } else if (!open && el.open) {
      el.close();
    }
  }, [open]);

  const matches = typed === requireTypeMatch;

  return (
    // <dialog> already handles Escape natively (fires onClose), so a
    // dedicated keyboard listener on the dialog itself would duplicate
    // the platform behaviour. The backdrop click is the only mouse
    // hook we add; jsx-a11y can't see the native Escape path, so we
    // suppress its companion warnings with a documented reason.
    // eslint-disable-next-line jsx-a11y/click-events-have-key-events, jsx-a11y/no-noninteractive-element-interactions
    <dialog
      ref={dialogRef}
      onClose={onCancel}
      onClick={(e) => {
        if (e.target === dialogRef.current) onCancel();
      }}
      aria-labelledby={titleId}
      aria-describedby={descId}
      className={cn(
        "bg-transparent p-0 m-auto backdrop:bg-black/60 backdrop:backdrop-blur-sm",
        "max-w-[calc(100vw-2rem)]",
      )}
    >
      <div className="w-[400px] max-w-full rounded bg-bg-card border border-border-hi p-5 flex flex-col gap-4">
        <div className="flex flex-col gap-1">
          <h2 id={titleId} className="text-base font-semibold text-fg">
            {title}
          </h2>
          <p id={descId} className="text-sm text-fg-muted leading-relaxed">
            {description}
          </p>
        </div>
        <label htmlFor={inputId} className="text-xs text-fg-muted flex flex-col gap-1.5">
          {t("typeToConfirm.prompt")} <span className="font-mono text-fg">{requireTypeMatch}</span>
          <Input
            ref={inputRef}
            id={inputId}
            value={typed}
            onChange={(e) => setTyped(e.target.value)}
            spellCheck={false}
            autoComplete="off"
            aria-invalid={typed.length > 0 && !matches}
          />
        </label>
        <div className="flex justify-end gap-2">
          <Button variant="ghost" size="sm" onClick={onCancel}>
            {cancelLabel}
          </Button>
          <Button
            variant={variant === "danger" ? "danger" : "default"}
            size="sm"
            onClick={onConfirm}
            disabled={!matches}
          >
            {confirmLabel}
          </Button>
        </div>
      </div>
    </dialog>
  );
}
