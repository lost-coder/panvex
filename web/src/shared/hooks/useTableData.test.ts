import { renderHook, act } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { useTableData } from "./useTableData";

describe("useTableData", () => {
  const items = Array.from({ length: 25 }, (_, i) => i);

  it("exposes 1-based pagination over the first page by default", () => {
    const { result } = renderHook(() => useTableData(items, 10));
    expect(result.current.page).toBe(1);
    expect(result.current.totalPages).toBe(3);
    expect(result.current.totalItems).toBe(25);
    expect(result.current.paginated).toEqual([0, 1, 2, 3, 4, 5, 6, 7, 8, 9]);
  });

  it("slices the requested 1-based page", () => {
    const { result } = renderHook(() => useTableData(items, 10));
    act(() => result.current.setPage(2));
    expect(result.current.page).toBe(2);
    expect(result.current.paginated).toEqual([10, 11, 12, 13, 14, 15, 16, 17, 18, 19]);
  });

  it("clamps the page when the filtered list shrinks below the current page", () => {
    const { result, rerender } = renderHook(({ data }) => useTableData(data, 10), {
      initialProps: { data: items },
    });
    act(() => result.current.setPage(3));
    expect(result.current.page).toBe(3);
    // Filtering down to 5 items leaves a single page — the hook must clamp
    // instead of stranding the caller on an empty "page 3 of 1".
    rerender({ data: items.slice(0, 5) });
    expect(result.current.page).toBe(1);
    expect(result.current.totalPages).toBe(1);
    expect(result.current.paginated).toEqual([0, 1, 2, 3, 4]);
  });
});
