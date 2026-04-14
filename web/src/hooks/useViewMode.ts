import { useState, useCallback } from "react";
import type { ViewMode } from "@lost-coder/panvex-ui";

const STORAGE_KEY = "panvex-view-mode";

export function useViewMode(section: string, autoThreshold: number = 10) {
  const storageKey = `${STORAGE_KEY}-${section}`;

  const [manualMode, setManualMode] = useState<ViewMode | null>(() => {
    const stored = localStorage.getItem(storageKey);
    return stored === "cards" || stored === "list" ? stored : null;
  });

  const setMode = useCallback(
    (mode: ViewMode) => {
      setManualMode(mode);
      localStorage.setItem(storageKey, mode);
    },
    [storageKey],
  );

  function resolveMode(itemCount: number): ViewMode {
    if (manualMode) return manualMode;
    return itemCount <= autoThreshold ? "cards" : "list";
  }

  return { manualMode, setMode, resolveMode };
}
