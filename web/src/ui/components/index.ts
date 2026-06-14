export { ErrorBoundary, type ErrorBoundaryProps } from "./ErrorBoundary";
export { AlertItem, type AlertItemProps, type AlertSeverity } from "./AlertItem";
export { TimelineEvent, type TimelineEventProps } from "./TimelineEvent";
export { SLABanner, type SLABannerProps } from "./SLABanner";
export { DataTable, type DataTableProps, type DataTableColumn } from "./DataTable";
export { EmptyState, type EmptyStateProps } from "./EmptyState";
export { SettingsGroup, type SettingsGroupProps } from "./SettingsGroup";
export { SettingsRow, type SettingsRowProps } from "./SettingsRow";
// Base-slot re-exports removed in 4e.5 — duplicates of @/ui/base/*
// caused TS2308 ambiguity at the root @/ui barrel. Consumers still
// reach them via the root barrel, which also exports ./base directly.
