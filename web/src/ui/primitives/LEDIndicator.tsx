import { cn } from "@/ui/lib/cn";
import type { Status } from "@/ui/tokens/colors";

export interface LEDIndicatorProps {
  status: Status;
  label: string;
  sublabel?: string;
  onClick?: () => void;
  active?: boolean;
  className?: string;
}

const ledColor = {
  ok: "bg-status-ok shadow-[0_0_6px_rgba(52,211,153,0.5)]",
  warn: "bg-status-warn shadow-[0_0_6px_rgba(245,158,11,0.5)]",
  error: "bg-status-error shadow-[0_0_6px_rgba(239,68,68,0.5)] animate-led-blink",
} as const;

export function LEDIndicator({
  status,
  label,
  sublabel,
  onClick,
  active = false,
  className,
}: LEDIndicatorProps) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={`${label}${sublabel ? ` — ${sublabel}` : ""}: ${status}`}
      aria-pressed={active}
      className={cn(
        "flex flex-col items-center gap-1.5 rounded-xs p-2 transition-colors",
        "hover:bg-bg-hover",
        active && "bg-bg-card-hi ring-1 ring-accent/30",
        className,
      )}
    >
      <span className={cn("h-3 w-3 rounded-full shrink-0", ledColor[status])} />
      <span className="text-xs font-mono font-medium text-fg leading-none">{label}</span>
      {sublabel && <span className="text-[10px] text-fg-muted leading-none">{sublabel}</span>}
    </button>
  );
}
