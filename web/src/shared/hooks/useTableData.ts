import { useMemo } from "react";
import { usePagination } from "./usePagination";

export interface UseTableDataResult<T> {
  /** Current page, 1-based and clamped — feed straight into TableView. */
  page: number;
  /** Set the page (1-based). Pass 1 to jump back to the start on filter change. */
  setPage: (page: number) => void;
  /** Total number of pages (≥ 1). */
  totalPages: number;
  /** Number of items after filtering (= the length passed in). */
  totalItems: number;
  /** The slice of `items` belonging to the current page. */
  paginated: T[];
}

/**
 * Table data orchestration: client-side pagination over an already-filtered
 * list, in the 1-based convention TableView's pagination config expects.
 *
 * This is the single adapter every list page should use instead of
 * hand-rolling `totalPages` / `safePage` / `slice`. It wraps usePagination
 * (the 0-based core) so the clamp-on-shrink behaviour — page snaps back into
 * range when filtering removes rows — is shared rather than re-implemented
 * (and occasionally forgotten) per page.
 */
export function useTableData<T>(
  items: readonly T[],
  pageSize: number,
): UseTableDataResult<T> {
  const pg = usePagination({ pageSize, totalCount: items.length });
  const paginated = useMemo(() => pg.slice(items), [pg, items]);
  return {
    page: pg.page + 1,
    setPage: (page) => pg.setPage(page - 1),
    totalPages: pg.pageCount,
    totalItems: items.length,
    paginated,
  };
}
