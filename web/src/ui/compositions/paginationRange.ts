/**
 * Pure helper for the {@link TableView} pager. Produces the sequence of
 * page tokens to render: page numbers plus `"ellipsis"` gap markers.
 *
 * The window keeps the first page, the last page, the current page, and
 * `siblingCount` pages on each side of the current one. Gaps wider than a
 * single page collapse into an ellipsis; a gap of exactly one page is
 * filled with that page instead (an ellipsis hiding a single number is
 * pointless and costs the operator a click).
 *
 * Examples (siblingCount = 1):
 *   total 20, current 6  -> [1, "ellipsis", 5, 6, 7, "ellipsis", 20]
 *   total 7,  current 4  -> [1, 2, 3, 4, 5, 6, 7]   (no gaps fit)
 *   total 20, current 1  -> [1, 2, 3, "ellipsis", 20]
 */
export type PaginationToken = number | "ellipsis";

export function computePaginationRange(
  currentPage: number,
  totalPages: number,
  siblingCount = 1,
): PaginationToken[] {
  if (totalPages <= 0) return [];

  const current = Math.min(Math.max(currentPage, 1), totalPages);

  // Boundaries always shown (first + last). The window spans
  // [current - siblingCount, current + siblingCount].
  const firstPage = 1;
  const lastPage = totalPages;
  const windowStart = Math.max(current - siblingCount, firstPage);
  const windowEnd = Math.min(current + siblingCount, lastPage);

  const pages = new Set<number>([firstPage, lastPage]);
  for (let p = windowStart; p <= windowEnd; p++) pages.add(p);

  const sorted = [...pages].sort((a, b) => a - b);

  const tokens: PaginationToken[] = [];
  let prev: number | undefined;
  for (const page of sorted) {
    if (prev !== undefined) {
      const gap = page - prev;
      if (gap === 2) {
        // Single missing page — render it rather than an ellipsis.
        tokens.push(prev + 1);
      } else if (gap > 2) {
        tokens.push("ellipsis");
      }
    }
    tokens.push(page);
    prev = page;
  }

  return tokens;
}
