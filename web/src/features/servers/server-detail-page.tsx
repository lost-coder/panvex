import { useParams, useRouter } from "@tanstack/react-router";
import { ArrowLeft, Server } from "lucide-react";
import { SectionPanel } from "@/components/section-panel";
import { Badge } from "@/components/ui/badge";
import { DcHealthBar } from "@/components/ui/dc-health-bar";
import { ActivityFeed } from "@/components/activity-feed";
import { useServers } from "./servers-state";

export function ServerDetailPage() {
  const { agentId } = useParams({ strict: false }) as { agentId: string };
  const router = useRouter();
  const { data: agents = [], isLoading } = useServers();
  const agent = agents.find((a) => a.id === agentId);

  if (isLoading) {
    return <div className="p-6"><div className="animate-pulse bg-surface h-8 w-48 rounded" /></div>;
  }

  if (!agent) {
    return (
      <div className="p-6">
        <button onClick={() => router.history.back()} className="flex items-center gap-2 text-text-3 hover:text-text-1 text-sm mb-4">
          <ArrowLeft className="w-4 h-4" /> Back
        </button>
        <p className="text-text-3">Server not found.</p>
      </div>
    );
  }

  const status = agent.presence_state;
  const variant = status === "online" ? "good" : status === "degraded" ? "warn" : "bad";
  const segment: "ok" | "partial" | "down" = status === "online" ? "ok" : status === "degraded" ? "partial" : "down";

  return (
    <div className="p-6 space-y-4">
      <button onClick={() => router.history.back()} className="flex items-center gap-2 text-text-3 hover:text-text-1 text-sm">
        <ArrowLeft className="w-4 h-4" /> Back to Servers
      </button>

      <div className="flex items-center gap-3">
        <h1 className="text-xl font-bold text-text-1">{agent.node_name}</h1>
        <Badge variant={variant}>{status}</Badge>
      </div>

      <SectionPanel icon={<Server className="w-4 h-4" />} title="Server Info">
        <div className="p-4 space-y-3">
          <div className="flex justify-between text-sm">
            <span className="text-text-3">ID</span>
            <span className="text-text-1 font-mono text-xs">{agent.id}</span>
          </div>
          <div className="flex justify-between text-sm">
            <span className="text-text-3">Version</span>
            <span className="text-text-1">{agent.version}</span>
          </div>
          <div className="flex justify-between text-sm">
            <span className="text-text-3">Fleet Group</span>
            <span className="text-text-1 font-mono text-xs">{agent.fleet_group_id}</span>
          </div>
          <div className="flex justify-between text-sm">
            <span className="text-text-3">Last Seen</span>
            <span className="text-text-1">{new Date(agent.last_seen_at).toLocaleString()}</span>
          </div>
          <div className="mt-2">
            <DcHealthBar segments={[segment]} />
          </div>
        </div>
      </SectionPanel>

      <SectionPanel title="Recent Events">
        <ActivityFeed items={[]} emptyMessage="No events recorded" />
      </SectionPanel>
    </div>
  );
}
