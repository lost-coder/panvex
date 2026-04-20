import { useEffect, useState } from "react";
import { cn } from "@/ui/lib/cn";

export type ToastVariant = "success" | "error" | "info";

export interface ToastProps {
  message: string;
  variant?: ToastVariant;
  duration?: number;
  open: boolean;
  onClose: () => void;
}

const variantStyles: Record<ToastVariant, string> = {
  success: "border-l-status-ok",
  error: "border-l-status-error",
  info: "border-l-accent",
};

const variantIcons: Record<ToastVariant, string> = {
  success: "✓",
  error: "✕",
  info: "ℹ",
};

export function Toast({ message, variant = "info", duration = 3000, open, onClose }: ToastProps) {
  const [visible, setVisible] = useState(false);

  useEffect(() => {
    if (open) {
      setVisible(true);
      const timer = setTimeout(() => {
        setVisible(false);
        setTimeout(onClose, 200);
      }, duration);
      return () => clearTimeout(timer);
    }
    setVisible(false);
  }, [open, duration, onClose]);

  if (!open && !visible) return null;

  return (
    <div
      // U3: expose the toast to assistive tech. Errors are announced
      // with `assertive` so they interrupt other readings; success and
      // info use `polite` so they do not clobber whatever the user was
      // reading. Hosts that stack several toasts inside one live
      // region may set aria-live on the wrapper and keep these
      // attributes for single-toast fallback usage — ARIA treats the
      // innermost live attribute as authoritative.
      role={variant === "error" ? "alert" : "status"}
      aria-live={variant === "error" ? "assertive" : "polite"}
      aria-atomic="true"
      className={cn(
        "fixed bottom-20 md:bottom-6 left-1/2 -translate-x-1/2 z-50",
        "flex items-center gap-2 rounded-xs bg-bg-card border border-border-hi border-l-[3px] px-4 py-3 shadow-xl",
        "transition-all duration-200",
        variantStyles[variant],
        visible ? "opacity-100 translate-y-0" : "opacity-0 translate-y-2",
      )}
    >
      <span
        className={cn(
          "text-sm shrink-0",
          variant === "success" && "text-status-ok",
          variant === "error" && "text-status-error",
          variant === "info" && "text-accent",
        )}
      >
        {variantIcons[variant]}
      </span>
      <span className="text-sm text-fg">{message}</span>
    </div>
  );
}
