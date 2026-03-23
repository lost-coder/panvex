import { useState } from "react";
import { SectionPanel } from "@/components/section-panel";
import { Toolbar } from "@/components/toolbar";
import { FilterChip } from "@/components/ui/filter-chip";
import { Server } from "lucide-react";
import { useServers } from "./servers-state";
import { ServerTable } from "./server-table";

type Filter = "all" | "online" | "offline" | "degraded";

export function ServersPage() {
  const { data: agents = [], isLoading } = useServers();
  const [filter, setFilter] = useState<Filter>("all");
  const [search, setSearch] = useState("");

  const filtered = agents.filter(a => {
    if (filter === "online" && a.presence_state !== "online") return false;
    if (filter === "offline" && a.presence_state !== "offline") return false;
    if (filter === "degraded" && a.presence_state !== "degraded") return false;
    if (search && !a.node_name.toLowerCase().includes(search.toLowerCase())) return false;
    return true;
  });

  const filters = (
    <div className="flex gap-2">
      {(["all", "online", "offline", "degraded"] as Filter[]).map(f => (
        <FilterChip
          key={f}
          label={f.charAt(0).toUpperCase() + f.slice(1)}
          active={filter === f}
          count={f === "all" ? agents.length : agents.filter(a => a.presence_state === f).length}
          onClick={() => setFilter(f)}
        />
      ))}
    </div>
  );

  return (
    <div className="p-6 space-y-4">
      <div>
        <h1 className="text-xl font-bold text-text-1">Servers</h1>
        <p className="text-sm text-text-3 mt-1">All enrolled agents in your fleet</p>
      </div>
      <Toolbar
        search={{ value: search, onChange: setSearch, placeholder: "Search servers..." }}
        filters={filters}
      />
      <SectionPanel icon={<Server className="w-4 h-4" />} title="Fleet">
        {isLoading ? (
          <div className="p-6 space-y-2">
            {[...Array(5)].map((_, i) => (
              <div key={i} className="animate-pulse bg-surface h-10 rounded" />
            ))}
          </div>
        ) : (
          <ServerTable agents={filtered} />
        )}
      </SectionPanel>
    </div>
  );
}
