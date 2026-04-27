// R-Q-08: paginated mobile/desktop body extracted from ClientsPage.tsx.
// Holds the responsive switch + the click-row plumbing in one place so
// the host page just hands a ready-to-render list to TableView.

import { DataTable, type ClientListItem } from "@/ui";

import { ClientListRow } from "./ClientListRow";
import type { ClientSelectionConfig } from "./ClientsTableColumns";

export interface ClientsTableBodyProps {
  rows: ClientListItem[];
  columns: Parameters<typeof DataTable<ClientListItem>>[0]["columns"];
  selection: ClientSelectionConfig;
  onClientClick?: ((id: string) => void) | undefined;
  nowMs: number;
}

export function ClientsTableBody({
  rows,
  columns,
  selection,
  onClientClick,
  nowMs,
}: Readonly<ClientsTableBodyProps>) {
  return (
    <div className="bg-bg-card border border-border rounded-xl shadow-sm overflow-hidden">
      {/* Mobile: compact rows with optional checkboxes. */}
      <div className="md:hidden flex flex-col">
        {rows.map((c) => (
          <ClientListRow
            key={c.id}
            client={c}
            onClick={() => onClientClick?.(c.id)}
            selectable
            selected={selection.selected.has(c.id)}
            onToggleSelect={selection.onToggle}
            nowMs={nowMs}
          />
        ))}
      </div>
      {/* Desktop: DataTable with multi-select column. */}
      <div className="hidden md:block">
        <DataTable
          columns={columns}
          data={rows}
          keyExtractor={(c) => c.id}
          onRowClick={(c) => onClientClick?.(c.id)}
        />
      </div>
    </div>
  );
}
