import {
  Badge,
  SectionHeader,
  Timeline,
  type DashboardTimelineData,
} from "@/ui";

export function TimelinePanel({ data }: Readonly<{ data: DashboardTimelineData }>) {
  if (!data?.events) return null;
  return (
    <div className="flex flex-col gap-2 bg-bg-card border border-border rounded-xs p-4">
      <SectionHeader title="Recent Events" trailing={<Badge variant="accent">live</Badge>} />
      <Timeline
        events={data.events.slice(0, 8).map((e) => ({
          status: e.status === "info" ? ("ok" as const) : e.status,
          time: e.time,
          message: e.message,
          source: e.source,
        }))}
      />
    </div>
  );
}
