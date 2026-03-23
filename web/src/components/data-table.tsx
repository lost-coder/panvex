import { cn } from "@/lib/cn";
import { ChevronUp, ChevronDown } from "lucide-react";
import { ReactNode } from "react";

interface Column<T> {
  key: string;
  label: string;
  sortable?: boolean;
  render: (row: T) => ReactNode;
}

interface DataTableProps<T> {
  columns: Column<T>[];
  rows: T[];
  sortKey?: string;
  sortDir?: "asc" | "desc";
  onSort?: (key: string) => void;
  onRowClick?: (row: T) => void;
  emptyMessage?: string;
}

export function DataTable<T extends { id?: string }>({ columns, rows, sortKey, sortDir, onSort, onRowClick, emptyMessage = "No data" }: DataTableProps<T>) {
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-border">
            {columns.map(col => (
              <th
                key={col.key}
                onClick={() => col.sortable && onSort?.(col.key)}
                className={cn("px-4 py-2.5 text-left text-[10px] font-bold uppercase tracking-[0.1em] text-text-3 whitespace-nowrap", col.sortable && "cursor-pointer hover:text-text-1 select-none")}
              >
                <span className="inline-flex items-center gap-1">
                  {col.label}
                  {col.sortable && sortKey === col.key && (
                    sortDir === "asc" ? <ChevronUp className="w-3 h-3" /> : <ChevronDown className="w-3 h-3" />
                  )}
                </span>
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.length === 0 ? (
            <tr><td colSpan={columns.length} className="px-4 py-8 text-center text-text-3 text-xs">{emptyMessage}</td></tr>
          ) : rows.map((row, i) => (
            <tr
              key={(row as any).id ?? i}
              onClick={() => onRowClick?.(row)}
              className={cn("border-b border-border transition-colors", onRowClick && "cursor-pointer hover:bg-row-hover", i % 2 === 1 && "bg-row-stripe")}
            >
              {columns.map(col => (
                <td key={col.key} className="px-4 py-3 text-text-1">{col.render(row)}</td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
