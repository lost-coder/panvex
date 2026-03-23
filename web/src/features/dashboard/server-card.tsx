import { Badge } from "@/components/ui/badge";
import { DcHealthBar } from "@/components/ui/dc-health-bar";

interface ServerCardProps {
  agent: any;
}

type BadgeVariant = "good" | "bad" | "warn";

function getStatusVariant(status: string): BadgeVariant {
  const s = (status ?? "").toLowerCase();
  if (s === "online" || s === "connected" || s === "active") return "good";
  if (s === "offline" || s === "disconnected" || s === "inactive") return "bad";
  return "warn";
}

function getStatusLabel(status: string): string {
  const s = (status ?? "").toLowerCase();
  if (s === "online" || s === "connected" || s === "active") return "Online";
  if (s === "offline" || s === "disconnected" || s === "inactive") return "Offline";
  return status ? status.charAt(0).toUpperCase() + status.slice(1) : "Unknown";
}

export function ServerCard({ agent }: ServerCardProps) {
  const name = agent.name ?? agent.hostname ?? agent.id ?? "Unknown Server";
  const status = agent.status ?? "unknown";
  const clientCount = agent.client_count ?? agent.clients ?? agent.live_connections ?? 0;
  const dcCoverage = agent.dc_coverage_pct ?? agent.coverage_pct ?? 0;

  return (
    <div className="bg-card border border-border rounded p-4 hover:bg-card-hover hover:border-border-hover transition-all cursor-pointer">
      <div className="flex items-center justify-between gap-2">
        <span className="text-sm font-bold text-text-1 truncate">{name}</span>
        <Badge variant={getStatusVariant(status)}>{getStatusLabel(status)}</Badge>
      </div>
      <div className="flex items-center gap-4 text-xs text-text-3 font-mono mt-2">
        <span>{clientCount} clients</span>
        <span>{dcCoverage}% coverage</span>
      </div>
      <div className="mt-3">
        <DcHealthBar segments={[agent.presence_state === "online" ? "ok" : agent.presence_state === "degraded" ? "partial" : "down"]} size="mini" />
      </div>
    </div>
  );
}
