import { cn } from "@/ui/lib/cn";

export interface GaugeCellProps {
  value: string;
  unit?: string;
  label: string;
  className?: string;
}

export function GaugeCell({ value, unit, label, className }: GaugeCellProps) {
  return (
    <div className={cn("flex flex-col gap-0.5 rounded-xs bg-bg-card p-3", className)}>
      <span className="text-lg font-mono font-semibold text-fg leading-none">
        {value}
        {unit && <span className="text-xs font-normal text-fg-muted ml-0.5">{unit}</span>}
      </span>
      <span className="text-caption uppercase tracking-wider leading-none">{label}</span>
    </div>
  );
}
