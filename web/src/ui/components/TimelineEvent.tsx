import { cn } from "@/ui/lib/cn";
import type { Status } from "@/ui/tokens/colors";

export interface TimelineEventProps {
  status: Status;
  time: string;
  message: string;
  detail?: string;
  /**
   * Optional source label (e.g. node name). When provided, it renders
   * on a first line alongside the timestamp; the message drops to a
   * second line and is free to wrap without fighting the time column
   * for horizontal space.
   */
  source?: string;
  className?: string;
}

const dotColor = {
  ok: "bg-status-ok",
  warn: "bg-status-warn",
  error: "bg-status-error",
} as const;

export function TimelineEvent({
  status,
  time,
  message,
  detail,
  source,
  className,
}: TimelineEventProps) {
  return (
    <div className={cn("flex items-start gap-3 py-2", className)}>
      {/* Rail column: narrow lane containing just the status dot, sized
          to match the absolute rail painted by the parent Timeline at
          `left-[4px]`. The dot is centered horizontally so it falls on
          the line, and `pt-1.5` aligns it with the first line of text. */}
      <div className="w-2 shrink-0 flex justify-center pt-1.5">
        <span className={cn("h-2 w-2 rounded-full shrink-0 relative z-10", dotColor[status])} />
      </div>
      <div className="flex-1 min-w-0">
        {source ? (
          <>
            {/* Two-line layout: source + time on top, message below. Keeps
                long messages from fighting a narrow timestamp column. */}
            <div className="flex items-baseline justify-between gap-2 min-w-0">
              <span className="text-xs font-mono text-accent truncate min-w-0">{source}</span>
              <span className="text-[11px] font-mono text-fg-muted shrink-0">{time}</span>
            </div>
            <p className="text-sm text-fg leading-snug break-words mt-0.5">{message}</p>
          </>
        ) : (
          <div className="flex items-baseline gap-2 min-w-0">
            <span className="text-[11px] font-mono text-fg-muted shrink-0">{time}</span>
            <span className="text-sm text-fg leading-snug break-words min-w-0 flex-1">
              {message}
            </span>
          </div>
        )}
        {detail && (
          <p className="text-xs text-fg-muted/70 mt-0.5 leading-relaxed break-words">
            {detail}
          </p>
        )}
      </div>
    </div>
  );
}
