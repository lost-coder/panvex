import { describe, it, expect } from "vitest";
import { computePaginationRange } from "./paginationRange";

describe("computePaginationRange", () => {
  it("returns empty for non-positive totals", () => {
    expect(computePaginationRange(1, 0)).toEqual([]);
    expect(computePaginationRange(1, -3)).toEqual([]);
  });

  it("lists every page when there are no gaps to collapse", () => {
    expect(computePaginationRange(4, 7)).toEqual([1, 2, 3, 4, 5, 6, 7]);
  });

  it("collapses both sides around a mid page", () => {
    expect(computePaginationRange(6, 20)).toEqual([
      1,
      "ellipsis",
      5,
      6,
      7,
      "ellipsis",
      20,
    ]);
  });

  it("does not collapse a single-page gap (renders the number instead)", () => {
    // current 3 of 5: window 2..4, boundaries 1 & 5 -> no ellipsis needed.
    expect(computePaginationRange(3, 5)).toEqual([1, 2, 3, 4, 5]);
  });

  it("collapses only the trailing side near the start", () => {
    expect(computePaginationRange(1, 20)).toEqual([1, 2, "ellipsis", 20]);
  });

  it("collapses only the leading side near the end", () => {
    expect(computePaginationRange(20, 20)).toEqual([1, "ellipsis", 19, 20]);
  });

  it("clamps out-of-range current page", () => {
    // 99 clamps to 5 (last): window 4..5, boundary 1 -> gap 1->4 collapses.
    expect(computePaginationRange(99, 5)).toEqual([1, "ellipsis", 4, 5]);
    // -1 clamps to 1 (first): window 1..2, boundary 5 -> gap 2->5 collapses.
    expect(computePaginationRange(-1, 5)).toEqual([1, 2, "ellipsis", 5]);
  });

  it("honours a wider sibling count", () => {
    expect(computePaginationRange(10, 20, 2)).toEqual([
      1,
      "ellipsis",
      8,
      9,
      10,
      11,
      12,
      "ellipsis",
      20,
    ]);
  });

  it("never duplicates first/last when window reaches the boundary", () => {
    expect(computePaginationRange(2, 4)).toEqual([1, 2, 3, 4]);
  });
});
