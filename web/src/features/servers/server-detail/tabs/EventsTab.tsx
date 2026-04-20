import { Badge } from "@/ui/primitives/Badge";
import { formatTime } from "@/ui/lib/format";
import { cn } from "@/ui/lib/cn";
import type { ServerDetailPageProps, ServerEventData } from "@/shared/api/types-pages/pages";

function eventTone(eventType: string): "ok" | "warn" | "error" | "info" {
  const t = eventType.toLowerCase();
  if (/error|fail|disconnect|offline|crash|down/.test(t)) return "error";
  if (/warn|timeout|retry|slow|degrad/.test(t)) return "warn";
  if (/connect|online|ready|recover/.test(t)) return "ok";
  return "info";
}

function EventRow({ event }: { event: ServerEventData }) {
  const tone = eventTone(event.eventType);
  // Left rail coloured per tone — the handoff "event log" look: time as a
  // mono timestamp, message leans on a subtle accent strip that says "this
  // is a warn/error/info/ok" without a separate badge.
  const rail =
    tone === "error"
      ? "bg-status-error"
      : tone === "warn"
        ? "bg-status-warn"
        : tone === "ok"
          ? "bg-status-ok"
          : "bg-fg-faint";
  return (
    <div className="flex gap-3 items-start py-2 border-b border-divider last:border-b-0">
      <span className={cn("w-[3px] self-stretch rounded-sm shrink-0", rail)} />
      <span className="text-[11px] font-mono text-fg-muted shrink-0 min-w-[80px] pt-0.5">
        {formatTime(event.tsEpochSecs)}
      </span>
      <div className="flex flex-col gap-0.5 min-w-0 flex-1">
        <span className="text-xs font-mono text-fg">{event.eventType}</span>
        {event.context && (
          <span className="text-[11px] font-mono text-fg-muted break-words">{event.context}</span>
        )}
      </div>
    </div>
  );
}

export function EventsTab({ server }: { server: ServerDetailPageProps["server"] }) {
  const { events, eventsDroppedTotal } = server;

  if (events.length === 0) {
    return (
      <div className="py-8 text-center text-sm text-fg-muted">No events.</div>
    );
  }

  return (
    <div className="flex flex-col gap-3">
      {eventsDroppedTotal > 0 && (
        <Badge variant="warn">⚠ {eventsDroppedTotal} events dropped</Badge>
      )}
      <div className="flex flex-col">
        {events.map((event) => (
          <EventRow key={event.seq} event={event} />
        ))}
      </div>
    </div>
  );
}
