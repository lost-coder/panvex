import { cn } from "@/ui/lib/cn";
import { TimelineEvent, type TimelineEventProps } from "@/ui/components/TimelineEvent";

export interface TimelineProps {
  events: TimelineEventProps[];
  className?: string;
}

export function Timeline({ events, className }: TimelineProps) {
  return (
    // Continuous rail + dots-in-column:
    // * Timeline is position:relative and draws ONE vertical line as an
    //   absolutely positioned span spanning its full height — no per-row
    //   fragments.
    // * TimelineEvent lays its dot inside a rail column of the same width
    //   that aligns with the line, so every dot sits on the line and the
    //   line runs through the whole list regardless of row padding or
    //   per-event content height.
    <div className={cn("relative flex flex-col", className)}>
      <span
        aria-hidden="true"
        className="absolute top-1 bottom-1 left-[4px] w-px bg-divider"
      />
      {events.map((event, index) => (
        <TimelineEvent
          // Source + index guarantee unique React keys even when the
          // backend feed carries repeated event type / time pairs.
          key={`${event.source ?? ""}-${event.time}-${event.message}-${index}`}
          {...event}
        />
      ))}
    </div>
  );
}
