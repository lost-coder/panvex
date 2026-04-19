import { useEffect, useId, useRef, useState } from "react";

import { Button } from "@/ui/base/button";
import { Input } from "@/ui/base/input";
import { cn } from "@/ui/lib/cn";

export interface TypeToConfirmDialogProps {
  open: boolean;
  title: string;
  description: string;
  /** The exact string the operator must type before confirm enables. */
  requireTypeMatch: string;
  confirmLabel?: string;
  cancelLabel?: string;
  variant?: "default" | "danger";
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
}: TypeToConfirmDialogProps) {
  const dialogRef = useRef<HTMLDialogElement>(null);
  const titleId = useId();
  const descId = useId();
  const inputId = useId();
  const [typed, setTyped] = useState("");

  useEffect(() => {
    const el = dialogRef.current;
    if (!el) return;
    if (open && !el.open) {
      el.showModal();
      setTyped("");
    } else if (!open && el.open) {
      el.close();
    }
  }, [open]);

  const matches = typed === requireTypeMatch;

  return (
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
          Чтобы подтвердить, введите <span className="font-mono text-fg">{requireTypeMatch}</span>
          <Input
            id={inputId}
            value={typed}
            onChange={(e) => setTyped(e.target.value)}
            autoFocus
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
