// R-Q-08: page-scoped multi-select helper extracted from ClientsPage.tsx.
// Owns the selected-set + toggle/toggleAll/clear bookkeeping so the host
// just consumes the returned helpers and selection config.

import { useMemo, useState } from "react";

export interface ClientSelectionApi {
  selected: Set<string>;
  allSelected: boolean;
  someSelected: boolean;
  toggleOne: (id: string) => void;
  toggleAllOnPage: () => void;
  clear: () => void;
}

export function useClientSelection(pageIds: string[]): ClientSelectionApi {
  const [selected, setSelected] = useState<Set<string>>(() => new Set());

  const { allSelected, someSelected } = useMemo(() => {
    const onPage = pageIds.filter((id) => selected.has(id));
    const all = pageIds.length > 0 && onPage.length === pageIds.length;
    return { allSelected: all, someSelected: onPage.length > 0 && !all };
  }, [pageIds, selected]);

  const toggleOne = (id: string) =>
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });

  const toggleAllOnPage = () =>
    setSelected((prev) => {
      const next = new Set(prev);
      if (allSelected) pageIds.forEach((id) => next.delete(id));
      else pageIds.forEach((id) => next.add(id));
      return next;
    });

  const clear = () => setSelected(new Set());

  return { selected, allSelected, someSelected, toggleOne, toggleAllOnPage, clear };
}
