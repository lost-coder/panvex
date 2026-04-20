import { cn } from "@/ui/lib/cn";
import type { Status } from "@/ui/tokens/colors";

export interface TimelineEventProps {
  status: Status;
  time: string;
  message: string;
  detail?: string;
  className?: string;
}

const dotColor = {
  ok: "bg-status-ok",
  warn: "bg-status-warn",
  error: "bg-status-error",
} as const;

export function TimelineEvent({ status, time, message, detail, className }: TimelineEventProps) {
  return (
    <div className={cn("flex gap-3 relative", className)}>
      <div className="flex flex-col items-center pt-1.5">
        <span className={cn("h-2 w-2 rounded-full shrink-0", dotColor[status])} />
        <span className="w-px flex-1 bg-fg-faint mt-1" />
      </div>
      <div className="pb-4 flex-1 min-w-0">
        <div className="flex items-baseline gap-2">
          <span className="text-[11px] font-mono text-fg-muted shrink-0">{time}</span>
          <span className="text-sm text-fg leading-snug">{message}</span>
        </div>
        {detail && <p className="text-xs text-fg-muted/70 mt-0.5 leading-relaxed">{detail}</p>}
      </div>
    </div>
  );
}
