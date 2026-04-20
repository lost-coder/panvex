import { cn } from "@/ui/lib/cn";
import { TimelineEvent, type TimelineEventProps } from "@/ui/components/TimelineEvent";

export interface TimelineProps {
  events: TimelineEventProps[];
  className?: string;
}

export function Timeline({ events, className }: TimelineProps) {
  return (
    <div className={cn("flex flex-col", className)}>
      {events.map((event) => (
        <TimelineEvent key={`${event.time}-${event.message}`} {...event} />
      ))}
    </div>
  );
}
