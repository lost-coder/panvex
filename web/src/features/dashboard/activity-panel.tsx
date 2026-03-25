import { Activity } from "lucide-react";
import { SectionPanel } from "@/components/section-panel";
import { ActivityFeed } from "@/components/activity-feed";
import type { ReactNode } from "react";
import type { Agent } from "@/lib/api";
import { extractRecentRuntimeEvents } from "./dashboard-view-model";

interface ActivityPanelProps {
  agents: Agent[];
}

export function ActivityPanel({ agents }: ActivityPanelProps) {
  const icon: ReactNode = <Activity className="w-4 h-4" />;
  const mapped = extractRecentRuntimeEvents(agents).map((event) => ({
    id: event.id,
    text: `${event.agentName}: ${event.summaryText}`,
    time: formatRelativeTimestamp(event.timestampUnix),
    status: event.status,
  }));

  return (
    <SectionPanel icon={icon} title="Recent Activity">
      <ActivityFeed emptyMessage="No runtime events reported" items={mapped} />
    </SectionPanel>
  );
}

function formatRelativeTimestamp(timestampUnix: number): string {
  const eventTimestamp = timestampUnix * 1000;
  if (!Number.isFinite(eventTimestamp) || eventTimestamp <= 0) {
    return "unknown";
  }

  const diffMs = Math.max(0, Date.now() - eventTimestamp);
  const diffMinutes = Math.round(diffMs / 60_000);

  if (diffMinutes < 1) {
    return "just now";
  }

  if (diffMinutes < 60) {
    return `${diffMinutes} min ago`;
  }

  const diffHours = Math.round(diffMinutes / 60);
  if (diffHours < 24) {
    return `${diffHours} hr ago`;
  }

  const diffDays = Math.round(diffHours / 24);
  return `${diffDays} d ago`;
}
