// Phase 4b: shared hooks used across multiple feature slices.
// NOTE: useUpdates is deliberately NOT re-exported here. It pulls the
// aggregated apiClient (all 12 domains' zod schemas), and this barrel is
// imported by entry-synchronous components (OfflineBanner, router shell) —
// re-exporting it hoists every schema into the entry chunk and blows the
// size-limit budget. Import it directly: `@/shared/hooks/useUpdates`.
export { useWsUpdateFlash } from "./useWsUpdateFlash";
export { useUrlSearchState } from "./useUrlSearchState";
export { useViewMode } from "./useViewMode";
export { useOnlineStatus } from "./useOnlineStatus";
export { useFocusMainOnRouteChange } from "./useFocusMainOnRouteChange";
export { useKeyboardShortcut } from "./useKeyboardShortcut";
export { useTableData } from "./useTableData";
export type { UseTableDataResult } from "./useTableData";
export { useRelativeTime, relativeTimeParts } from "./useRelativeTime";
export { useIsDesktop } from "./useIsDesktop";
export { useUnsavedChangesGuard } from "./useUnsavedChangesGuard";
