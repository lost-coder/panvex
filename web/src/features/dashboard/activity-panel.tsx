import { Activity } from "lucide-react";
import { SectionPanel } from "@/components/section-panel";
import { ActivityFeed } from "@/components/activity-feed";
import type { ReactNode } from "react";

interface ActivityPanelProps {
  agents: any[];
}

type ActivityStatus = "good" | "bad" | "warn" | "accent";

function mapEventTypeToStatus(eventType: string): ActivityStatus {
  const type = (eventType ?? "").toLowerCase();
  if (
    type.includes("connect") ||
    type.includes("online") ||
    type.includes("join") ||
    type.includes("register")
  ) {
    return "good";
  }
  if (
    type.includes("error") ||
    type.includes("fail") ||
    type.includes("disconnect") ||
    type.includes("offline") ||
    type.includes("crash")
  ) {
    return "bad";
  }
  if (
    type.includes("warn") ||
    type.includes("timeout") ||
    type.includes("retry") ||
    type.includes("slow")
  ) {
    return "warn";
  }
  return "accent";
}

export function ActivityPanel({ agents }: ActivityPanelProps) {
  // Collect events from all agents
  const allEvents: any[] = [];
  for (const agent of agents) {
    const events = agent.recent_events ?? agent.events ?? [];
    for (const event of events) {
      allEvents.push({ ...event, _agentId: agent.id, _agentName: agent.name ?? agent.hostname });
    }
  }

  // Sort by timestamp descending and take top 20
  allEvents.sort((a, b) => {
    const ta = new Date(a.timestamp ?? a.created_at ?? 0).getTime();
    const tb = new Date(b.timestamp ?? b.created_at ?? 0).getTime();
    return tb - ta;
  });

  const top20 = allEvents.slice(0, 20);

  const mapped = top20.map((event) => ({
    id: event.id ?? `${event._agentId}-${event.timestamp}`,
    text: event.message ?? event.description ?? event.event_type ?? "Event",
    time: event.timestamp ?? event.created_at ?? "",
    status: mapEventTypeToStatus(event.event_type ?? event.type ?? ""),
  }));

  const icon: ReactNode = <Activity className="w-4 h-4" />;

  return (
    <SectionPanel icon={icon} title="Activity">
      <ActivityFeed items={mapped} />
    </SectionPanel>
  );
}
