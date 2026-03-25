import { cn } from "@/lib/cn";
import { ChevronUp, ChevronDown } from "lucide-react";
import { ReactNode } from "react";

interface Column<T> {
  key: string;
  label: string;
  sortable?: boolean;
  headerClassName?: string;
  cellClassName?: string;
  mobileLabel?: string;
  responsiveClassName?: string;
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
  footer?: ReactNode;
  headerRowClassName?: string;
  rowClassName?: string | ((row: T, index: number) => string | undefined);
  tableClassName?: string;
  wrapperClassName?: string;
}

export function DataTable<T extends { id?: string }>({
  columns,
  rows,
  sortKey,
  sortDir,
  onSort,
  onRowClick,
  emptyMessage = "No data",
  footer,
  headerRowClassName,
  rowClassName,
  tableClassName,
  wrapperClassName,
}: DataTableProps<T>) {
  return (
    <div className={wrapperClassName}>
      <div className="overflow-x-auto">
        <table className={cn("w-full text-sm", tableClassName)}>
          <thead>
            <tr className={cn("border-b border-border", headerRowClassName)}>
              {columns.map(col => (
                <th
                  key={col.key}
                  onClick={() => col.sortable && onSort?.(col.key)}
                  className={cn(
                    "px-4 py-2.5 text-left text-[10px] font-bold uppercase tracking-[0.1em] text-text-3 whitespace-nowrap",
                    col.sortable && "cursor-pointer hover:text-text-1 select-none",
                    col.headerClassName,
                    col.responsiveClassName
                  )}
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
              <tr>
                <td colSpan={columns.length} className="px-4 py-8 text-center text-text-3 text-xs">
                  {emptyMessage}
                </td>
              </tr>
            ) : rows.map((row, i) => (
              <tr
                key={row.id ?? i}
                onClick={() => onRowClick?.(row)}
                className={cn(
                  "border-b border-border transition-colors",
                  onRowClick && "cursor-pointer hover:bg-row-hover",
                  i % 2 === 1 && "bg-row-stripe",
                  typeof rowClassName === "function" ? rowClassName(row, i) : rowClassName
                )}
              >
                {columns.map(col => (
                  <td
                    key={col.key}
                    className={cn("px-4 py-3 text-text-1", col.cellClassName, col.responsiveClassName)}
                    data-mobile-label={col.mobileLabel}
                  >
                    {col.render(row)}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {footer}
    </div>
  );
}
