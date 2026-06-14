import { useTranslation } from "react-i18next";

import { NodeSummaryCard } from "@/features/servers/ui/NodeSummaryCard";
import { localizeReason, type ServerListItem } from "@/ui";

export function ServerCardView({
  servers,
  onServerClick,
}: Readonly<{
  servers: ServerListItem[];
  onServerClick?: ((id: string) => void) | undefined;
}>) {
  const { t: tc } = useTranslation("common");
  return (
    <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
      {servers.map((s) => (
        <NodeSummaryCard
          key={s.id}
          name={s.name}
          status={s.status}
          state={s.state}
          reason={s.reason ? localizeReason(s.reason, tc) : ""}
          connections={s.connections}
          trafficBytes={s.trafficBytes}
          cpuPct={s.cpuPct}
          memPct={s.memPct}
          dcs={s.dcs || []}
          onClick={() => onServerClick?.(s.id)}
        />
      ))}
    </div>
  );
}
