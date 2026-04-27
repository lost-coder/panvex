import type { ReactNode } from "react";
import { cn } from "@/ui/lib/cn";

export interface ActionItemProps {
  /**
   * Icon slot. Accepts any ReactNode so callers can pass a Lucide
   * component, an emoji, or a custom SVG without wrapping the string
   * in a component. (U5 — was `string`, tightened to ReactNode.)
   */
  icon: ReactNode;
  label: string;
  description: string;
  variant?: "default" | "danger";
  onClick?: () => void;
  className?: string;
}

export function ActionItem({
  icon,
  label,
  description,
  variant = "default",
  onClick,
  className,
}: Readonly<ActionItemProps>) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "flex items-center gap-3 w-full rounded-xs px-3 py-2.5 text-left transition-colors",
        "hover:bg-bg-hover",
        "active:bg-bg-card-hi",
        className,
      )}
    >
      <span
        className={cn(
          "flex items-center justify-center h-9 w-9 rounded-xs shrink-0 text-base",
          variant === "danger"
            ? "bg-status-error/10 text-status-error"
            : "bg-accent/10 text-accent",
        )}
      >
        {icon}
      </span>
      <div className="flex flex-col flex-1 min-w-0">
        <span
          className={cn(
            "text-sm font-medium leading-none",
            variant === "danger" ? "text-status-error" : "text-fg",
          )}
        >
          {label}
        </span>
        <span className="text-caption mt-0.5 leading-snug truncate">{description}</span>
      </div>
      <span className="text-fg-muted/40 text-sm">›</span>
    </button>
  );
}
