import { cn } from "@/ui/lib/cn";

export interface ProgressBarProps {
  value: number;
  max?: number;
  label?: string;
  showValue?: boolean;
  size?: "sm" | "md";
  /** "threshold" colors by value (green/warn/red), "info" always uses accent */
  variant?: "threshold" | "info";
  className?: string;
}

function getColor(pct: number, variant: "threshold" | "info"): string {
  if (variant === "info") return "bg-accent";
  if (pct >= 90) return "bg-status-error";
  if (pct >= 70) return "bg-status-warn";
  return "bg-status-ok";
}

export function ProgressBar({
  value,
  max = 100,
  label,
  showValue = true,
  size = "md",
  variant = "threshold",
  className,
}: ProgressBarProps) {
  const pct = Math.min(100, Math.max(0, (value / max) * 100));

  return (
    <div className={cn("flex flex-col gap-1", className)}>
      {(label || showValue) && (
        <div className="flex items-baseline justify-between">
          {label && <span className="text-caption uppercase tracking-wider">{label}</span>}
          {showValue && <span className="text-xs font-mono text-fg">{pct.toFixed(1)}%</span>}
        </div>
      )}
      <div
        className={cn(
          "w-full rounded-full bg-fg-faint overflow-hidden",
          size === "sm" ? "h-1.5" : "h-2.5",
        )}
      >
        <div
          className={cn("h-full rounded-full transition-all duration-500", getColor(pct, variant))}
          style={{ width: `${pct}%` }}
        />
      </div>
    </div>
  );
}
