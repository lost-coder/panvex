import { Badge } from "@/ui/primitives/Badge";
import { DataTable } from "@/ui/components/DataTable";
import { formatTime } from "@/ui/lib/format";
import type { ServerDetailPageProps, ServerEventData } from "@/shared/api/types-pages/pages";

export function EventsTab({ server }: { server: ServerDetailPageProps["server"] }) {
  const { events, eventsDroppedTotal } = server;

  const eventColumns = [
    {
      key: "time",
      header: "Time",
      render: (row: ServerEventData) => (
        <span className="font-mono text-xs text-fg-muted">{formatTime(row.tsEpochSecs)}</span>
      ),
      className: "w-24",
    },
    {
      key: "type",
      header: "Type",
      render: (row: ServerEventData) => <Badge variant="default">{row.eventType}</Badge>,
    },
    {
      key: "context",
      header: "Context",
      render: (row: ServerEventData) => <span className="text-xs text-fg">{row.context}</span>,
    },
  ];

  return (
    <div className="flex flex-col gap-4 pt-2">
      {eventsDroppedTotal > 0 && (
        <Badge variant="warn">⚠ {eventsDroppedTotal} events dropped</Badge>
      )}

      <DataTable
        columns={eventColumns}
        data={events}
        keyExtractor={(row) => String(row.seq)}
        emptyMessage="No events"
      />
    </div>
  );
}
