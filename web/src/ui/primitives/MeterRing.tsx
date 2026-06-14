import { cn } from "@/ui/lib/cn";

export interface MeterRingProps {
  value: number;
  max?: number;
  label?: string;
  size?: number;
  className?: string;
}

function getStroke(pct: number): string {
  if (pct >= 90) return "#ef4444";
  if (pct >= 70) return "#f59e0b";
  return "#34d399";
}

export function MeterRing({ value, max = 100, label, size = 72, className }: Readonly<MeterRingProps>) {
  const pct = Math.min(100, Math.max(0, (value / max) * 100));
  const r = (size - 8) / 2;
  const circ = 2 * Math.PI * r;
  const offset = circ * (1 - pct / 100);

  return (
    <div className={cn("inline-flex flex-col items-center gap-1", className)}>
      <div className="relative" style={{ width: size, height: size }}>
        <svg width={size} height={size} className="-rotate-90">
          <circle
            cx={size / 2}
            cy={size / 2}
            r={r}
            fill="none"
            stroke="currentColor"
            strokeWidth={4}
            className="text-fg-faint"
          />
          <circle
            cx={size / 2}
            cy={size / 2}
            r={r}
            fill="none"
            stroke={getStroke(pct)}
            strokeWidth={4}
            strokeLinecap="round"
            strokeDasharray={circ}
            strokeDashoffset={offset}
            className="transition-all duration-700"
          />
        </svg>
        <div className="absolute inset-0 flex items-center justify-center">
          <span className="text-sm font-mono font-bold text-fg">{Math.round(pct)}%</span>
        </div>
      </div>
      {label && <span className="text-nano text-fg-muted uppercase tracking-wider">{label}</span>}
    </div>
  );
}
