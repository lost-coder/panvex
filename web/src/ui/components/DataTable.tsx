import { useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useVirtualizer } from "@tanstack/react-virtual";
import { cn } from "@/ui/lib/cn";

export interface DataTableColumn<T> {
  key: string;
  header: string;
  render: (row: Readonly<T>) => React.ReactNode;
  /**
   * Marks the header as sortable (renders a clickable, keyboard-operable
   * affordance + aria-sort). To actually reorder rows you must also supply
   * `sortValue` — `render` returns ReactNode and can't be compared on.
   */
  sortable?: boolean;
  /**
   * Comparable value the table sorts on when this column is active. Numbers
   * compare numerically; everything else compares as a locale-aware,
   * case-insensitive string. null/undefined always sort last regardless of
   * direction. Omit on a `sortable` column only if the parent sorts the
   * `data` prop itself (controlled sorting).
   */
  sortValue?: (row: Readonly<T>) => string | number | null | undefined;
  className?: string;
}

/**
 * Comparator for two column sort values. Numbers compare numerically; mixed
 * or string values fall back to a numeric-aware, case-insensitive locale
 * compare so "item2" < "item10" and "Alpha" sorts next to "alpha".
 */
function compareSortValues(
  a: string | number | null | undefined,
  b: string | number | null | undefined,
): number {
  if (typeof a === "number" && typeof b === "number") return a - b;
  return String(a).localeCompare(String(b), undefined, {
    numeric: true,
    sensitivity: "base",
  });
}

export interface DataTableProps<T> {
  columns: DataTableColumn<T>[];
  data: T[];
  keyExtractor: (row: Readonly<T>) => string;
  onRowClick?: (row: Readonly<T>) => void;
  emptyMessage?: string;
  /**
   * Accessible name for the table, rendered as a visually-hidden
   * `<caption>` and mirrored onto `aria-label`. Strongly recommended so
   * screen-reader users get table context (axe: data tables should have a
   * descriptive accessible name).
   */
  caption?: string;
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
  emptyMessage,
  caption,
  className,
  rowHeight = 48,
  maxHeight = 600,
}: Readonly<DataTableProps<T>>) {
  const { t } = useTranslation();
  // U3: don't hardcode an English default. `t` resolves against the
  // "common" namespace (i18n defaultNS); `common.empty` gives a localized
  // "No data" fallback when the caller doesn't pass emptyMessage.
  const resolvedEmpty = emptyMessage ?? t("empty");
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

  // Apply the active sort. A column is only sortable-with-effect when it
  // provides `sortValue`; otherwise (no active column, or controlled
  // sorting upstream) we render `data` untouched. The decorate-sort-
  // undecorate keeps the sort stable so equal keys preserve input order.
  const sortedData = useMemo(() => {
    if (!sortKey) return data;
    const activeCol = columns.find((c) => c.key === sortKey);
    const accessor = activeCol?.sortValue;
    if (!accessor) return data;
    const dir = sortDir === "asc" ? 1 : -1;
    return data
      .map((row, index) => ({ row, index }))
      .sort((a, b) => {
        const av = accessor(a.row);
        const bv = accessor(b.row);
        const aNil = av === null || av === undefined;
        const bNil = bv === null || bv === undefined;
        // null/undefined always sink to the bottom, independent of dir.
        if (aNil || bNil) {
          if (aNil && bNil) return a.index - b.index;
          return aNil ? 1 : -1;
        }
        const cmp = compareSortValues(av, bv);
        return cmp !== 0 ? cmp * dir : a.index - b.index;
      })
      .map((d) => d.row);
  }, [data, columns, sortKey, sortDir]);

  // U3: rows that navigate must be keyboard-operable. A <tr> can't be a
  // <button>/<a> child of <tbody>, so we expose the button role +
  // Enter/Space activation directly on the row, mirroring NodeSummaryCard.
  const handleRowKeyDown = (e: React.KeyboardEvent, row: Readonly<T>) => {
    if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      onRowClick?.(row);
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
    count: sortedData.length,
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
        <table className="w-full table-fixed text-sm" aria-label={caption}>
          {caption && <caption className="sr-only">{caption}</caption>}
          <thead className="sticky top-0 z-10 bg-bg">
            <tr className="border-b border-border">
              {columns.map((col) => {
                // P2-FE-07 / U3: sortable headers must expose sort state to
                // assistive tech and be operable by keyboard. `scope="col"`
                // anchors each header to its column; `aria-sort` reflects
                // the active direction, with inactive sortable columns
                // reporting "none". The clickable affordance is a real
                // <button> inside the <th> so it gets focus + Enter/Space.
                const isActiveSort = col.sortable && sortKey === col.key;
                const ariaSort: "ascending" | "descending" | "none" | undefined = (() => {
                  if (!col.sortable) return undefined;
                  if (!isActiveSort) return "none";
                  return sortDir === "asc" ? "ascending" : "descending";
                })();
                return (
                  <th
                    key={col.key}
                    scope="col"
                    aria-sort={ariaSort}
                    className={cn(
                      "text-left text-caption uppercase tracking-wider font-medium px-3 py-2 bg-bg",
                      col.className,
                    )}
                  >
                    {col.sortable ? (
                      <button
                        type="button"
                        onClick={() => handleSort(col.key)}
                        className="inline-flex items-center gap-1 select-none uppercase tracking-wider font-medium hover:text-fg"
                      >
                        {col.header}
                        {isActiveSort && (
                          <span className="text-accent" aria-hidden="true">
                            {sortDir === "asc" ? "↑" : "↓"}
                          </span>
                        )}
                      </button>
                    ) : (
                      col.header
                    )}
                  </th>
                );
              })}
            </tr>
          </thead>
          <tbody>
            {sortedData.length === 0 ? (
              <tr>
                <td colSpan={columns.length} className="text-center text-fg-muted py-8">
                  {resolvedEmpty}
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
                  const row = sortedData[virtualRow.index];
                  // The virtualizer is driven by `count: data.length`, so
                  // every rendered virtual index maps to a real row. The
                  // guard satisfies noUncheckedIndexedAccess without
                  // complicating the render path.
                  if (!row) return null;
                  return (
                    <tr
                      key={keyExtractor(row)}
                      data-index={virtualRow.index}
                      onClick={onRowClick ? () => onRowClick(row) : undefined}
                      onKeyDown={onRowClick ? (e) => handleRowKeyDown(e, row) : undefined}
                      role={onRowClick ? "button" : undefined}
                      tabIndex={onRowClick ? 0 : undefined}
                      style={{ height: rowHeight }}
                      className={cn(
                        "border-b border-border/50 transition-colors",
                        onRowClick &&
                          "cursor-pointer hover:bg-bg-hover focus-visible:outline-2 focus-visible:outline-accent",
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
        {sortedData.length === 0 ? (
          <p className="text-center text-fg-muted py-8 text-sm">{resolvedEmpty}</p>
        ) : (
          sortedData.map((row) => {
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
