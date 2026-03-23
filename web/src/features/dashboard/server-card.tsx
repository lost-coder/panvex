import { useState } from "react";
import { ChevronDown, Activity, Wifi, Users } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { DcHealthBar } from "@/components/ui/dc-health-bar";
import { cn } from "@/lib/cn";
import type { Agent } from "@/lib/api";

function getStatusVariant(state: string): "good" | "bad" | "warn" {
  if (state === "online") return "good";
  if (state === "offline") return "bad";
  return "warn";
}

function getSegment(state: string): "ok" | "partial" | "down" {
  if (state === "online") return "ok";
  if (state === "offline") return "down";
  return "partial";
}

export function ServerCard({ agent }: { agent: Agent }) {
  const [open, setOpen] = useState(false);
  const state = agent.presence_state ?? "unknown";
  const segment = getSegment(state);

  return (
    <div
      className={cn(
        "bg-card border border-border rounded transition-all cursor-pointer",
        open ? "border-border-active" : "hover:border-border-hover"
      )}
      onClick={() => setOpen(!open)}
    >
      {/* Front */}
      <div className="p-4">
        <div className="flex items-center justify-between gap-2">
          <span className="text-sm font-bold text-text-1 truncate">{agent.node_name}</span>
          <div className="flex items-center gap-2 shrink-0">
            <Badge variant={getStatusVariant(state)} size="sm">{state}</Badge>
            <ChevronDown className={cn("w-3.5 h-3.5 text-text-3 transition-transform duration-200", open && "rotate-180")} />
          </div>
        </div>
        <div className="flex items-center gap-3 text-[11px] text-text-3 font-mono mt-2">
          <span className="flex items-center gap-1"><Wifi className="w-3 h-3" />{agent.runtime?.me_runtime_ready ? "ready" : "not ready"}</span>
          <span className="flex items-center gap-1"><Users className="w-3 h-3" />{agent.runtime?.accepting_new_connections ? "accepting" : "closed"}</span>
        </div>
        <div className="mt-3">
          <DcHealthBar segments={[segment]} size="mini" />
        </div>
      </div>

      {/* Expanded back */}
      {open && (
        <div
          className="border-t border-border bg-card-back px-4 py-3 space-y-2"
          onClick={e => e.stopPropagation()}
        >
          <div className="flex items-center gap-1 text-[10px] font-bold uppercase tracking-widest text-text-3 mb-2">
            <Activity className="w-3 h-3" /> Details
          </div>
          <div className="grid grid-cols-2 gap-x-4 gap-y-1 text-[11px]">
            <span className="text-text-3">Version</span>
            <span className="text-text-2 font-mono">{agent.version}</span>
            <span className="text-text-3">Fleet group</span>
            <span className="text-text-2 font-mono truncate">{agent.fleet_group_id || "—"}</span>
            <span className="text-text-3">Last seen</span>
            <span className="text-text-2 font-mono">{agent.last_seen_at ? new Date(agent.last_seen_at).toLocaleTimeString() : "—"}</span>
            <span className="text-text-3">Read-only</span>
            <span className="text-text-2">{agent.read_only ? "Yes" : "No"}</span>
          </div>
        </div>
      )}
    </div>
  );
}
