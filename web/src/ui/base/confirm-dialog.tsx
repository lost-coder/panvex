import { useEffect, useId, useRef } from "react";
import { cn } from "@/ui/lib/cn";
import { Button } from "./button";

export interface ConfirmDialogProps {
  open: boolean;
  title: string;
  description: string;
  confirmLabel?: string | undefined;
  cancelLabel?: string | undefined;
  variant?: "default" | "danger" | undefined;
  onConfirm: () => void;
  onCancel: () => void;
}

export function ConfirmDialog({
  open,
  title,
  description,
  confirmLabel = "Confirm",
  cancelLabel = "Cancel",
  variant = "default",
  onConfirm,
  onCancel,
}: Readonly<ConfirmDialogProps>) {
  const dialogRef = useRef<HTMLDialogElement>(null);
  const titleId = useId();
  const descId = useId();

  useEffect(() => {
    const el = dialogRef.current;
    if (!el) return;
    if (open && !el.open) el.showModal();
    else if (!open && el.open) el.close();
  }, [open]);

  return (
    // <dialog> already handles Escape natively (fires onClose) — a
    // dedicated keyboard listener on the dialog itself would just
    // duplicate the platform behaviour. The backdrop click below is
    // the only mouse hook we add; jsx-a11y can't see the native
    // Escape path so we suppress its companion warnings here with a
    // documented reason.
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
      <div className="w-[360px] max-w-full rounded bg-bg-card border border-border-hi p-5 flex flex-col gap-4">
        <div className="flex flex-col gap-1">
          <h2 id={titleId} className="text-base font-semibold text-fg">
            {title}
          </h2>
          <p id={descId} className="text-sm text-fg-muted leading-relaxed">
            {description}
          </p>
        </div>
        <div className="flex justify-end gap-2">
          <Button variant="ghost" size="sm" onClick={onCancel}>
            {cancelLabel}
          </Button>
          <Button
            variant={variant === "danger" ? "danger" : "default"}
            size="sm"
            onClick={onConfirm}
          >
            {confirmLabel}
          </Button>
        </div>
      </div>
    </dialog>
  );
}
