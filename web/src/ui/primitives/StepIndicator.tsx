// src/primitives/StepIndicator.tsx
import { cn } from "@/ui/lib/cn";

function stepClass(i: number, current: number): string {
  if (i < current) return "bg-status-ok text-white";
  if (i === current) return "bg-accent text-white";
  return "bg-bg-card border border-border text-fg-muted";
}

export interface StepIndicatorProps {
  steps: string[];
  current: number;
  className?: string;
}

export function StepIndicator({ steps, current, className }: Readonly<StepIndicatorProps>) {
  return (
    <div className={cn("flex items-center gap-2", className)}>
      {steps.map((label, i) => (
        <div key={label} className="flex items-center gap-2">
          <div className="flex items-center gap-1.5">
            <div
              className={cn(
                "w-6 h-6 rounded-full flex items-center justify-center text-xs font-medium",
                stepClass(i, current),
              )}
            >
              {i < current ? "✓" : i + 1}
            </div>
            <span
              className={cn(
                "text-xs hidden sm:inline",
                i === current ? "text-fg font-medium" : "text-fg-muted",
              )}
            >
              {label}
            </span>
          </div>
          {i < steps.length - 1 && (
            <div className={cn("w-8 h-px", i < current ? "bg-status-ok" : "bg-border")} />
          )}
        </div>
      ))}
    </div>
  );
}
