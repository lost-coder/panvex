import { useRef, useState } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { cn } from "@/ui/lib/cn";

export interface DataTableColumn<T> {
  key: string;
  header: string;
  render: (row: Readonly<T>) => React.ReactNode;
  sortable?: boolean;
  className?: string;
}

export interface DataTableProps<T> {
  columns: DataTableColumn<T>[];
  data: T[];
  keyExtractor: (row: Readonly<T>) => string;
  onRowClick?: (row: Readonly<T>) => void;
  emptyMessage?: string;
  className?: string;
  /**
   * Row height in pixels used by the virtualizer to estimate the scroll
   * range. Default: 48 (matches px-3 py-2.5 density used by callers).
   * P3-PERF-02: virtualization avoids rendering thousands of DOM rows.
   */
  rowHeight?: number;
  /**
   * Maximum container height in pixels. The scroll parent is clamped to
   * this value so the virtualizer has a bounded viewport. Default: 600.
   */
  maxHeight?: number;
}

type SortDir = "asc" | "desc";

export function DataTable<T>({
  columns,
  data,
  keyExtractor,
  onRowClick,
  emptyMessage = "No data",
  className,
  rowHeight = 48,
  maxHeight = 600,
}: Readonly<DataTableProps<T>>) {
  const [sortKey, setSortKey] = useState<string | null>(null);
  const [sortDir, setSortDir] = useState<SortDir>("asc");
  const parentRef = useRef<HTMLDivElement | null>(null);

  const handleSort = (key: string) => {
    if (sortKey === key) setSortDir((d) => (d === "asc" ? "desc" : "asc"));
    else {
      setSortKey(key);
      setSortDir("asc");
    }
  };

  // P3-PERF-02: virtualize the desktop table body. With 5000 agents we
  // previously emitted 5000 <tr> nodes; now only the rows intersecting the
  // viewport (plus overscan) are rendered, keeping scroll at 60 FPS.
  //
  // R-Q-24: TanStack Virtual's API returns memoization-incompatible
  // functions (compiler skips memoization here). Accepted by-design —
  // suppress the warning so it does not drown legitimate signals.
  // eslint-disable-next-line react-hooks/incompatible-library
  const rowVirtualizer = useVirtualizer({
    count: data.length,
    getScrollElement: () => parentRef.current,
    estimateSize: () => rowHeight,
    overscan: 6,
  });

  const virtualItems = rowVirtualizer.getVirtualItems();
  const totalSize = rowVirtualizer.getTotalSize();
  const firstVirtual = virtualItems[0];
  const lastVirtual = virtualItems[virtualItems.length - 1];
  const paddingTop = firstVirtual ? firstVirtual.start : 0;
  const paddingBottom = lastVirtual ? totalSize - lastVirtual.end : 0;

  return (
    <>
      {/* Desktop table — virtualized scroll container with sticky header. */}
      <div
        ref={parentRef}
        className={cn("hidden md:block overflow-auto", className)}
        style={{ maxHeight }}
      >
        <table className="w-full table-fixed text-sm">
          <thead className="sticky top-0 z-10 bg-bg">
            <tr className="border-b border-border">
              {columns.map((col) => {
                // P2-FE-07 / M-F6: sortable headers must expose sort state to
                // assistive tech. `scope="col"` anchors each header to its
                // column; `aria-sort` reflects the active direction, with
                // inactive sortable columns reporting "none" so a screen
                // reader can surface "sortable but not currently sorted".
                const isActiveSort = col.sortable && sortKey === col.key;
                const ariaSort: "ascending" | "descending" | "none" | undefined = col.sortable
                  ? isActiveSort
                    ? sortDir === "asc"
                      ? "ascending"
                      : "descending"
                    : "none"
                  : undefined;
                return (
                  <th
                    key={col.key}
                    scope="col"
                    aria-sort={ariaSort}
                    className={cn(
                      "text-left text-caption uppercase tracking-wider font-medium px-3 py-2 bg-bg",
                      col.sortable && "cursor-pointer select-none hover:text-fg",
                      col.className,
                    )}
                    onClick={col.sortable ? () => handleSort(col.key) : undefined}
                  >
                    {col.header}
                    {isActiveSort && (
                      <span className="ml-1 text-accent">{sortDir === "asc" ? "↑" : "↓"}</span>
                    )}
                  </th>
                );
              })}
            </tr>
          </thead>
          <tbody>
            {data.length === 0 ? (
              <tr>
                <td colSpan={columns.length} className="text-center text-fg-muted py-8">
                  {emptyMessage}
                </td>
              </tr>
            ) : (
              <>
                {paddingTop > 0 && (
                  <tr style={{ height: paddingTop }}>
                    <td colSpan={columns.length} />
                  </tr>
                )}
                {virtualItems.map((virtualRow) => {
                  const row = data[virtualRow.index];
                  // The virtualizer is driven by `count: data.length`, so
                  // every rendered virtual index maps to a real row. The
                  // guard satisfies noUncheckedIndexedAccess without
                  // complicating the render path.
                  if (!row) return null;
                  return (
                    <tr
                      key={keyExtractor(row)}
                      data-index={virtualRow.index}
                      onClick={() => onRowClick?.(row)}
                      style={{ height: rowHeight }}
                      className={cn(
                        "border-b border-border/50 transition-colors",
                        onRowClick && "cursor-pointer hover:bg-bg-hover",
                      )}
                    >
                      {columns.map((col) => (
                        <td key={col.key} className={cn("px-3 py-2.5", col.className)}>
                          {col.render(row)}
                        </td>
                      ))}
                    </tr>
                  );
                })}
                {paddingBottom > 0 && (
                  <tr style={{ height: paddingBottom }}>
                    <td colSpan={columns.length} />
                  </tr>
                )}
              </>
            )}
          </tbody>
        </table>
      </div>

      {/* Mobile card view. Only wrap the row in a <button> when a row-click
          handler is supplied — otherwise any interactive element inside a
          column (e.g. a Revoke button) would create invalid nested-button
          DOM and swallow click events meant for the inner control. */}
      <div className={cn("flex flex-col gap-2 md:hidden", className)}>
        {data.length === 0 ? (
          <p className="text-center text-fg-muted py-8 text-sm">{emptyMessage}</p>
        ) : (
          data.map((row) => {
            const content = (
              <div className="flex flex-col gap-1.5">
                {columns.map((col) => (
                  <div key={col.key} className="flex items-center justify-between gap-2">
                    <span className="text-[10px] text-fg-muted uppercase tracking-wider shrink-0">
                      {col.header}
                    </span>
                    <span className="text-sm text-fg text-right truncate">{col.render(row)}</span>
                  </div>
                ))}
              </div>
            );
            const baseCls =
              "rounded-xs bg-bg-card p-3 text-left border border-transparent transition-colors";
            return onRowClick ? (
              <button
                key={keyExtractor(row)}
                type="button"
                onClick={() => onRowClick(row)}
                className={cn(baseCls, "hover:border-border-hi")}
              >
                {content}
              </button>
            ) : (
              <div key={keyExtractor(row)} className={baseCls}>
                {content}
              </div>
            );
          })
        )}
      </div>
    </>
  );
}
