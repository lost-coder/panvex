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

export function Toast({ message, variant = "info", duration = 3000, open, onClose }: Readonly<ToastProps>) {
  const [visible, setVisible] = useState(false);

  useEffect(() => {
    // R-Q-24: setVisible synchronises the visibility flag with the
    // `open` prop driving entry/exit animation. Using a state here
    // (rather than deriving directly from `open`) is intentional so
    // the close transition has time to play before unmount. The
    // cascading-render the rule warns about is the desired behaviour.
    if (open) {
      // eslint-disable-next-line react-hooks/set-state-in-effect
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

  const wrapperClass = cn(
    "fixed bottom-20 md:bottom-6 left-1/2 -translate-x-1/2 z-50",
    "flex items-center gap-2 rounded-xs bg-bg-card border border-border-hi border-l-[3px] px-4 py-3 shadow-xl",
    "transition-all duration-200",
    variantStyles[variant],
    visible ? "opacity-100 translate-y-0" : "opacity-0 translate-y-2",
  );
  const body = (
    <>
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
    </>
  );

  // U3: expose the toast to assistive tech. Errors get role="alert" so
  // they interrupt the current reading; success and info ride a
  // semantic <output> live region (default role=status, polite). Both
  // wrappers carry aria-atomic so partial updates do not get re-read.
  return variant === "error" ? (
    <div role="alert" aria-live="assertive" aria-atomic="true" className={wrapperClass}>
      {body}
    </div>
  ) : (
    <output aria-live="polite" aria-atomic="true" className={wrapperClass}>
      {body}
    </output>
  );
}
