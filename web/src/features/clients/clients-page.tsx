import { useState } from "react";
import { useClients } from "./clients-state";
import { Toolbar } from "@/components/toolbar";
import { FilterChip } from "@/components/ui/filter-chip";
import { DataTable } from "@/components/data-table";
import { Pagination } from "@/components/pagination";
import { Badge } from "@/components/ui/badge";
import type { ClientListItem } from "@/lib/api";

const PAGE_SIZE = 20;

function clientStatus(c: ClientListItem): "good" | "warn" | "bad" {
  if (!c.enabled) return "bad";
  if (c.active_tcp_conns > 0) return "good";
  return "warn";
}

export function ClientsPage() {
  const { data: clients = [], isLoading } = useClients();
  const [search, setSearch] = useState("");
  const [filter, setFilter] = useState<"all" | "active" | "inactive">("all");
  const [page, setPage] = useState(1);

  const filtered = clients.filter((c) => {
    const matchSearch = c.name.toLowerCase().includes(search.toLowerCase());
    const matchFilter =
      filter === "all" ||
      (filter === "active" && c.enabled && c.active_tcp_conns > 0) ||
      (filter === "inactive" && (!c.enabled || c.active_tcp_conns === 0));
    return matchSearch && matchFilter;
  });

  const totalPages = Math.ceil(filtered.length / PAGE_SIZE);
  const paged = filtered.slice((page - 1) * PAGE_SIZE, page * PAGE_SIZE);

  const columns = [
    { key: "name", label: "Name", render: (c: ClientListItem) => <span className="font-semibold text-text-1">{c.name}</span> },
    { key: "status", label: "Status", render: (c: ClientListItem) => <Badge variant={clientStatus(c)}>{c.enabled ? "Active" : "Disabled"}</Badge> },
    { key: "conns", label: "Connections", render: (c: ClientListItem) => <span className="font-mono text-text-2">{c.active_tcp_conns}</span> },
    { key: "servers", label: "Servers", render: (c: ClientListItem) => <span className="text-text-2">{c.assigned_nodes_count ?? "—"}</span> },
    { key: "traffic", label: "Traffic", render: (c: ClientListItem) => <span className="font-mono text-text-3">{(c.traffic_used_bytes / 1e9).toFixed(2)} GB</span> },
  ];

  return (
    <div className="p-5">
      <div className="mb-5">
        <h1 className="text-xl font-extrabold text-text-1">Clients</h1>
        <p className="text-sm text-text-3 mt-0.5">Manage connected clients</p>
      </div>
      <Toolbar
        search={{ value: search, onChange: (v) => { setSearch(v); setPage(1); }, placeholder: "Search clients..." }}
        filters={
          <div className="flex gap-2">
            {(["all", "active", "inactive"] as const).map((f) => (
              <FilterChip key={f} label={f.charAt(0).toUpperCase() + f.slice(1)} active={filter === f} onClick={() => { setFilter(f); setPage(1); }} />
            ))}
          </div>
        }
      />
      {isLoading ? (
        <div className="space-y-2">{[...Array(5)].map((_, i) => <div key={i} className="animate-pulse bg-surface h-10 rounded" />)}</div>
      ) : (
        <>
          <DataTable columns={columns} rows={paged} />
          <Pagination page={page} totalPages={totalPages} onPage={setPage} />
        </>
      )}
    </div>
  );
}
