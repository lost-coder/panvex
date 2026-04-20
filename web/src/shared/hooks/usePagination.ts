import { useMemo, useState } from "react";

export interface UsePaginationOptions {
  /** Number of items per page. */
  pageSize: number;
  /** Total items after filtering. The page clamps if this shrinks. */
  totalCount: number;
  /** Initial page (defaults to 0). */
  initialPage?: number;
}

export interface UsePaginationResult {
  /** Current page, clamped to [0, pageCount - 1]. Read this, not `rawPage`. */
  page: number;
  /** Setter — call with a new page index, or via `setPage(p => ...)`. */
  setPage: (next: number | ((prev: number) => number)) => void;
  /** Go to previous page. No-op on page 0. */
  prev: () => void;
  /** Go to next page. No-op on last page. */
  next: () => void;
  /** Total number of pages (≥ 1). */
  pageCount: number;
  /** 0-based index of the first item on the current page. */
  rangeStart: number;
  /** 0-based index of the last item on the current page (inclusive). */
  rangeEnd: number;
  /** True when `page > 0`. */
  hasPrev: boolean;
  /** True when `page < pageCount - 1`. */
  hasNext: boolean;
  /** Helper to slice an array to the current page. */
  slice: <T>(items: readonly T[]) => T[];
}

/**
 * Client-side pagination state. Clamps the page to the valid range on every
 * read so filter changes that shrink `totalCount` don't strand the user on
 * "page 8 of 3" — no useEffect required.
 *
 * Typical call site:
 * ```ts
 * const { page, setPage, pageCount, slice } = usePagination({ pageSize: 50, totalCount: filtered.length });
 * const pageRows = slice(filtered);
 * ```
 */
export function usePagination({
  pageSize,
  totalCount,
  initialPage = 0,
}: UsePaginationOptions): UsePaginationResult {
  const [rawPage, setRawPage] = useState(initialPage);

  const pageCount = Math.max(1, Math.ceil(totalCount / pageSize));
  const page = Math.min(Math.max(0, rawPage), pageCount - 1);

  const rangeStart = page * pageSize;
  const rangeEnd = Math.min(rangeStart + pageSize, totalCount) - 1;

  return useMemo<UsePaginationResult>(
    () => ({
      page,
      setPage: (next) =>
        setRawPage((prev) => (typeof next === "function" ? next(prev) : next)),
      prev: () => setRawPage((p) => Math.max(0, p - 1)),
      next: () => setRawPage((p) => Math.min(pageCount - 1, p + 1)),
      pageCount,
      rangeStart,
      rangeEnd,
      hasPrev: page > 0,
      hasNext: page < pageCount - 1,
      slice: <T,>(items: readonly T[]) =>
        items.slice(page * pageSize, (page + 1) * pageSize),
    }),
    [page, pageCount, pageSize, rangeStart, rangeEnd],
  );
}
