import { NodeSummaryCard } from "@/features/servers/ui/NodeSummaryCard";
import type { ServerListItem } from "@/ui";

export function ServerCardView({
  servers,
  onServerClick,
}: Readonly<{
  servers: ServerListItem[];
  onServerClick?: ((id: string) => void) | undefined;
}>) {
  return (
    <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
      {servers.map((s) => (
        <div key={s.id} className="flex flex-col gap-1">
          {s.telemtReachable === false && (
            <div className="text-xs font-mono text-red-400 px-1">⚠ Telemt недоступен</div>
          )}
          <NodeSummaryCard
            name={s.name}
            status={s.status}
            connections={s.connections}
            trafficBytes={s.trafficBytes}
            cpuPct={s.cpuPct}
            memPct={s.memPct}
            dcs={s.dcs || []}
            onClick={() => onServerClick?.(s.id)}
          />
        </div>
      ))}
    </div>
  );
}
