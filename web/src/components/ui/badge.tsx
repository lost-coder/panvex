import { cn } from "@/lib/cn";
import type { ReactNode } from "react";

const variantClasses = {
  good: "bg-good-dim text-good-text",
  warn: "bg-warn-dim text-warn-text",
  bad: "bg-bad-dim text-bad-text",
  accent: "bg-accent-dim text-accent-bright",
};

const dotColors = {
  good: "bg-good",
  warn: "bg-warn",
  bad: "bg-bad",
  accent: "bg-accent",
};

const sizeClasses = {
  default: "px-2.5 py-0.5 text-[11px]",
  sm: "px-2 py-px text-[10px]",
};

interface BadgeProps {
  variant: "good" | "warn" | "bad" | "accent";
  size?: "default" | "sm";
  dot?: boolean;
  children: ReactNode;
}

export function Badge({ variant, size = "default", dot, children }: BadgeProps) {
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 font-semibold rounded-full font-sans",
        sizeClasses[size],
        variantClasses[variant]
      )}
    >
      {dot && (
        <span className={cn("w-1.5 h-1.5 rounded-full", dotColors[variant])} />
      )}
      {children}
    </span>
  );
}
