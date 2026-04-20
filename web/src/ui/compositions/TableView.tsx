import * as React from "react";
import { Search, ChevronLeft, ChevronRight, Columns3 } from "lucide-react";
import { cn } from "@/ui/lib/cn";
import { Input } from "@/ui/base/input";
import { Select } from "@/ui/base/select";
import { Popover, PopoverTrigger, PopoverContent } from "@/ui/base/popover";
import { ViewModeToggle } from "@/ui/compositions/ViewModeToggle";
import type { ViewMode } from "@/shared/api/types-pages/pages";

export interface TableViewFilter {
  key: string;
  value: string;
  onChange: (val: string) => void;
  options: Array<{ value: string; label: string; tone?: "ok" | "warn" | "error" }>;
  placeholder?: string;
  /** "select" (default) renders a dropdown; "chips" renders an inline
   *  toggle-group so common statuses are one click away. */
  variant?: "select" | "chips";
}

export interface TableViewColumn {
  key: string;
  label: string;
}

export interface TableViewSearchConfig {
  value: string;
  onChange: (val: string) => void;
  placeholder?: string;
}

export interface TableViewPaginationConfig {
  page: number;
  totalPages: number;
  totalItems?: number;
  pageSize?: number;
  onChange: (page: number) => void;
}

export interface TableViewViewModeConfig {
  current: ViewMode;
  onChange: (mode: ViewMode) => void;
}

export interface TableViewColumnsConfig {
  available: TableViewColumn[];
  visibility: Record<string, boolean>;
  onChange: (key: string, visible: boolean) => void;
}

export interface TableViewProps {
  search?: TableViewSearchConfig;
  filters?: TableViewFilter[];
  viewMode?: TableViewViewModeConfig;
  columns?: TableViewColumnsConfig;
  pagination?: TableViewPaginationConfig;
  children: React.ReactNode;
  className?: string;
}

function Divider() {
  return <div className="w-px self-stretch bg-border" />;
}

export function TableView({
  search,
  filters,
  viewMode,
  columns,
  pagination,
  children,
  className,
}: TableViewProps) {
  const hasFilters = filters && filters.length > 0;
  const hasViewMode = viewMode !== undefined;
  const hasColumnPicker = columns !== undefined && columns.available.length > 0;

  // Derived pagination display
  const currentPage = pagination?.page ?? 1;
  const showPagination = pagination !== undefined && pagination.totalPages > 1;
  const rangeStart =
    pagination?.totalItems !== undefined && pagination?.pageSize !== undefined
      ? (currentPage - 1) * pagination.pageSize + 1
      : undefined;
  const rangeEnd =
    pagination?.totalItems !== undefined && pagination?.pageSize !== undefined
      ? Math.min(currentPage * pagination.pageSize, pagination.totalItems)
      : undefined;

  return (
    <div className={cn("flex flex-col gap-4", className)}>
      {/* Toolbar */}
      <div className="flex flex-col sm:flex-row gap-2 bg-bg-card p-2 rounded-xl border border-border">
        {/* Search — capped so it doesn't push the filters off to the edge
            on wide monitors. */}
        <div className="relative w-full sm:w-64 md:w-72">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 size-3.5 text-fg-muted pointer-events-none" />
          <Input
            type="search"
            value={search?.value ?? ""}
            onChange={(e) => search?.onChange(e.target.value)}
            placeholder={search?.placeholder ?? "Search…"}
            className="pl-9"
          />
        </div>

        {/* Right controls */}
        {(hasFilters || hasColumnPicker || hasViewMode) && (
          <div className="flex gap-2 items-center flex-wrap sm:ml-auto">
            {/* Filters */}
            {hasFilters &&
              filters!.map((f) =>
                f.variant === "chips" ? (
                  <div
                    key={f.key}
                    role="tablist"
                    aria-label={f.placeholder ?? "Filter"}
                    className="inline-flex items-center gap-0.5 p-0.5 rounded-xs border border-border-hi bg-bg overflow-x-auto"
                  >
                    {f.options.map((o) => {
                      const active = o.value === f.value;
                      const toneDot =
                        o.tone === "ok"
                          ? "bg-status-ok"
                          : o.tone === "warn"
                            ? "bg-status-warn"
                            : o.tone === "error"
                              ? "bg-status-error"
                              : "";
                      return (
                        <button
                          key={o.value}
                          type="button"
                          role="tab"
                          aria-selected={active}
                          onClick={() => f.onChange(o.value)}
                          className={cn(
                            "flex items-center gap-1.5 h-8 px-3 rounded-xs text-[11px] font-mono whitespace-nowrap transition-colors",
                            active
                              ? "bg-bg-card-hi text-fg"
                              : "text-fg-muted hover:text-fg hover:bg-bg-hover",
                          )}
                        >
                          {toneDot && (
                            <span
                              aria-hidden="true"
                              className={cn("h-1.5 w-1.5 rounded-full", toneDot)}
                            />
                          )}
                          {o.label}
                        </button>
                      );
                    })}
                  </div>
                ) : (
                  <Select
                    key={f.key}
                    value={f.value}
                    onChange={f.onChange}
                    options={f.options}
                    placeholder={f.placeholder ?? "All"}
                  />
                ),
              )}

            {/* Divider before column picker / view mode */}
            {hasFilters && (hasColumnPicker || hasViewMode) && <Divider />}

            {/* Column visibility picker — desktop only; mobile lists
                already collapse to a card view where per-column toggles
                would be meaningless. */}
            {hasColumnPicker && columns && (
              <Popover>
                <PopoverTrigger asChild>
                  <button
                    className={cn(
                      "hidden sm:flex items-center justify-center h-10 w-10 rounded-xs border border-border-hi",
                      "bg-bg-card text-fg-muted hover:text-fg transition-colors",
                      "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/50",
                    )}
                    aria-label="Toggle columns"
                  >
                    <Columns3 className="size-4" />
                  </button>
                </PopoverTrigger>
                <PopoverContent align="end" className="w-52 p-3">
                  <p className="text-xs font-medium text-fg-muted uppercase tracking-wider mb-2">
                    Columns
                  </p>
                  <div className="flex flex-col gap-1.5">
                    {columns.available.map((col) => {
                      const visible = columns.visibility[col.key] ?? true;
                      return (
                        <label
                          key={col.key}
                          className="flex items-center gap-2 cursor-pointer select-none text-sm text-fg"
                        >
                          <input
                            type="checkbox"
                            checked={visible}
                            onChange={(e) => columns.onChange(col.key, e.target.checked)}
                            className="accent-accent"
                          />
                          {col.label}
                        </label>
                      );
                    })}
                  </div>
                </PopoverContent>
              </Popover>
            )}

            {/* Divider between column picker and view mode */}
            {hasColumnPicker && hasViewMode && <Divider />}

            {/* View mode toggle — hidden on mobile */}
            {hasViewMode && viewMode && (
              <div className="hidden sm:block">
                <ViewModeToggle mode={viewMode.current} onChange={viewMode.onChange} />
              </div>
            )}
          </div>
        )}
      </div>

      {/* Content */}
      {children}

      {/* Pagination */}
      {showPagination && (
        <div className="flex items-center justify-between px-1">
          <span className="text-xs text-fg-muted font-mono">
            {rangeStart !== undefined &&
            rangeEnd !== undefined &&
            pagination?.totalItems !== undefined
              ? `Showing ${rangeStart}–${rangeEnd} of ${pagination.totalItems}`
              : `Page ${currentPage} of ${pagination!.totalPages}`}
          </span>

          <div className="flex gap-1">
            <button
              onClick={() => pagination?.onChange(currentPage - 1)}
              disabled={currentPage <= 1}
              className={cn(
                "flex items-center justify-center h-8 w-8 rounded-xs border border-border-hi",
                "bg-bg-card text-fg-muted hover:text-fg transition-colors",
                "disabled:opacity-40 disabled:cursor-not-allowed",
                "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/50",
              )}
              aria-label="Previous page"
            >
              <ChevronLeft className="size-4" />
            </button>

            {/* Page numbers */}
            {Array.from({ length: pagination!.totalPages }, (_, i) => i + 1).map((page) => (
              <button
                key={page}
                onClick={() => pagination?.onChange(page)}
                className={cn(
                  "flex items-center justify-center h-8 w-8 rounded-xs border font-mono text-xs transition-colors",
                  "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/50",
                  page === currentPage
                    ? "bg-accent border-accent text-white"
                    : "bg-bg-card border-border-hi text-fg-muted hover:text-fg",
                )}
                aria-label={`Page ${page}`}
                aria-current={page === currentPage ? "page" : undefined}
              >
                {page}
              </button>
            ))}

            <button
              onClick={() => pagination?.onChange(currentPage + 1)}
              disabled={currentPage >= pagination!.totalPages}
              className={cn(
                "flex items-center justify-center h-8 w-8 rounded-xs border border-border-hi",
                "bg-bg-card text-fg-muted hover:text-fg transition-colors",
                "disabled:opacity-40 disabled:cursor-not-allowed",
                "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/50",
              )}
              aria-label="Next page"
            >
              <ChevronRight className="size-4" />
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
