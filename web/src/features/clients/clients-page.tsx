import { useState } from "react";
import { List } from "lucide-react";
import { Pagination } from "@/components/pagination";
import { Toolbar } from "@/components/toolbar";
import { FilterChip } from "@/components/ui/filter-chip";
import { ViewToggle } from "@/components/ui/view-toggle";
import { ClientTable } from "./client-table";
import { useClients } from "./clients-state";
import {
  buildClientFilterCounts,
  buildClientTableRows,
  filterClientTableRows,
  paginateClientTableRows,
  sortClientTableRows,
  type ClientTableFilter,
  type ClientTableSortDir,
  type ClientTableSortKey,
} from "./clients-view-model";

const PAGE_SIZE = 8;

export function ClientsPage() {
  const { data: clients = [], isLoading, isError } = useClients();
  const [filter, setFilter] = useState<ClientTableFilter>("all");
  const [search, setSearch] = useState("");
  const [page, setPage] = useState(1);
  const [sortKey, setSortKey] = useState<ClientTableSortKey>("client");
  const [sortDir, setSortDir] = useState<ClientTableSortDir>("asc");

  const rows = buildClientTableRows(clients);
  const filterCounts = buildClientFilterCounts(rows);
  const filteredRows = filterClientTableRows(rows, { filter, search });
  const sortedRows = sortClientTableRows(filteredRows, sortKey, sortDir);
  const paginatedRows = paginateClientTableRows(sortedRows, page, PAGE_SIZE);
  const currentPage = Math.min(page, paginatedRows.totalPages);
  const pageStart = sortedRows.length === 0 ? 0 : (currentPage - 1) * PAGE_SIZE + 1;
  const pageEnd = sortedRows.length === 0 ? 0 : Math.min(currentPage * PAGE_SIZE, sortedRows.length);

  const filters = (
    <div className="flex gap-2">
      {([
        ["all", "All"],
        ["active", "Active"],
        ["idle", "Idle"],
        ["disabled", "Disabled"],
      ] as Array<[ClientTableFilter, string]>).map(([filterKey, label]) => (
        <FilterChip
          key={filterKey}
          active={filter === filterKey}
          count={filterCounts[filterKey]}
          label={label}
          onClick={() => {
            setFilter(filterKey);
            setPage(1);
          }}
        />
      ))}
    </div>
  );

  const footer = (
    <div className="client-table-footer">
      <div className="client-table-footer__info">
        Showing <strong>{pageStart}-{pageEnd}</strong> of <strong>{sortedRows.length}</strong> clients
      </div>
      <div className="client-table-footer__controls">
        <Pagination page={currentPage} totalPages={paginatedRows.totalPages} onPage={setPage} />
      </div>
    </div>
  );

  return (
    <div className="space-y-4 p-5">
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 className="text-[22px] font-extrabold tracking-[-0.03em] text-text-1">Clients</h1>
          <p className="mt-1 text-sm text-text-3">Manage connected clients</p>
        </div>
      </header>

      <Toolbar
        filters={filters}
        search={{
          onChange: (value) => {
            setSearch(value);
            setPage(1);
          },
          placeholder: "Search clients",
          value: search,
        }}
        viewToggle={
          <ViewToggle
            current="table"
            onChange={() => undefined}
            views={[{ key: "table", icon: List }]}
          />
        }
      />

      {isLoading ? (
        <div className="space-y-2">
          {[...Array(6)].map((_, index) => (
            <div key={index} className="h-14 animate-pulse rounded bg-surface" />
          ))}
        </div>
      ) : isError ? (
        <div className="rounded border border-bad/30 bg-bad-dim px-4 py-3 text-sm font-semibold text-bad-text">
          Clients data is unavailable.
        </div>
      ) : (
        <ClientTable
          footer={footer}
          onSort={(nextSortKey) => {
            setPage(1);
            if (sortKey === nextSortKey) {
              setSortDir((currentValue) => currentValue === "asc" ? "desc" : "asc");
              return;
            }
            setSortKey(nextSortKey);
            setSortDir("asc");
          }}
          rows={paginatedRows.rows}
          sortDir={sortDir}
          sortKey={sortKey}
        />
      )}
    </div>
  );
}
