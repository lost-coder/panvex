import { cn } from "@/ui/lib/cn";
import type { Status } from "@/ui/tokens/colors";

export interface TimelineEventProps {
  status: Status;
  time: string;
  message: string;
  detail?: string | undefined;
  /**
   * Optional source label (e.g. node name). When provided, it renders
   * on a first line alongside the timestamp; the message drops to a
   * second line and is free to wrap without fighting the time column
   * for horizontal space.
   */
  source?: string | undefined;
  className?: string | undefined;
}

// Severity glyph + color — matches the fleet StatusPill vocabulary so an
// event reads the same way as a node row. Shape carries meaning, not just
// color (color-blind safe).
const glyph: Record<Status, { ch: string; color: string }> = {
  ok: { ch: "✓", color: "text-status-ok" },
  warn: { ch: "▲", color: "text-status-warn" },
  error: { ch: "⛔", color: "text-status-error" },
};

export function TimelineEvent({
  status,
  time,
  message,
  detail,
  source,
  className,
}: Readonly<TimelineEventProps>) {
  return (
    <div className={cn("flex items-start gap-3 py-2", className)}>
      {/* Rail column: narrow lane containing just the status glyph, sized
          to match the absolute rail painted by the parent Timeline at
          `left-[4px]`. The glyph is centered horizontally so it falls on
          the line, and `pt-0.5` aligns it with the first line of text. */}
      <div className="w-4 shrink-0 flex justify-center pt-0.5">
        <span aria-hidden="true" className={cn("text-[13px] leading-none relative z-10", glyph[status].color)}>
          {glyph[status].ch}
        </span>
      </div>
      <div className="flex-1 min-w-0">
        {source ? (
          <>
            {/* Two-line layout: source + time on top, message below. Keeps
                long messages from fighting a narrow timestamp column. */}
            <div className="flex items-baseline justify-between gap-2 min-w-0">
              <span className="text-xs font-mono text-accent truncate min-w-0">{source}</span>
              <span className="text-xs font-mono text-fg-muted shrink-0">{time}</span>
            </div>
            <p className="text-[15px] text-fg leading-snug break-words mt-0.5">{message}</p>
          </>
        ) : (
          <div className="flex items-baseline gap-2 min-w-0">
            <span className="text-xs font-mono text-fg-muted shrink-0">{time}</span>
            <span className="text-[15px] text-fg leading-snug break-words min-w-0 flex-1">
              {message}
            </span>
          </div>
        )}
        {detail && (
          <p className="text-xs text-fg-muted mt-0.5 leading-relaxed break-words">
            {detail}
          </p>
        )}
      </div>
    </div>
  );
}
