import type { Agent } from "@/lib/api";
import { Badge } from "@/components/ui/badge";
import { DcHealthBar } from "@/components/ui/dc-health-bar";
import { cn } from "@/lib/cn";

interface ServerTableProps {
  agents: Agent[];
  onRowClick?: (agent: Agent) => void;
}

function getStatusVariant(state: string): "good" | "warn" | "bad" {
  if (state === "online") return "good";
  if (state === "offline") return "bad";
  return "warn";
}

function getSegment(state: string): "ok" | "partial" | "down" {
  if (state === "online") return "ok";
  if (state === "offline") return "down";
  return "partial";
}

export function ServerTable({ agents, onRowClick }: ServerTableProps) {
  if (agents.length === 0) {
    return (
      <div className="px-4 py-12 text-center text-text-3 text-sm">
        No servers found.
      </div>
    );
  }

  return (
    <table className="w-full text-sm">
      <thead>
        <tr className="border-b border-border">
          <th className="px-4 py-2 text-left text-[10px] font-bold uppercase tracking-[0.1em] text-text-3">Node</th>
          <th className="px-4 py-2 text-left text-[10px] font-bold uppercase tracking-[0.1em] text-text-3">Status</th>
          <th className="px-4 py-2 text-left text-[10px] font-bold uppercase tracking-[0.1em] text-text-3">Version</th>
          <th className="px-4 py-2 text-left text-[10px] font-bold uppercase tracking-[0.1em] text-text-3">Health</th>
          <th className="px-4 py-2 text-left text-[10px] font-bold uppercase tracking-[0.1em] text-text-3">Last Seen</th>
        </tr>
      </thead>
      <tbody>
        {agents.map((agent, i) => (
          <tr
            key={agent.id}
            onClick={() => onRowClick?.(agent)}
            className={cn(
              "border-b border-border transition-colors cursor-pointer",
              i % 2 === 1 && "bg-row-stripe",
              "hover:bg-row-hover"
            )}
          >
            <td className="px-4 py-3 font-mono text-[13px] text-text-1 font-semibold">{agent.node_name}</td>
            <td className="px-4 py-3">
              <Badge variant={getStatusVariant(agent.presence_state)}>
                {agent.presence_state}
              </Badge>
            </td>
            <td className="px-4 py-3 font-mono text-[12px] text-text-3">{agent.version}</td>
            <td className="px-4 py-3 w-32">
              <DcHealthBar segments={[getSegment(agent.presence_state)]} size="mini" />
            </td>
            <td className="px-4 py-3 text-[12px] text-text-3">
              {agent.last_seen_at ? new Date(agent.last_seen_at).toLocaleString() : "—"}
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
