import * as React from "react";
import { useTranslation } from "react-i18next";
import { Search, ChevronLeft, ChevronRight, Columns3 } from "lucide-react";
import { cn } from "@/ui/lib/cn";
import { Input } from "@/ui/base/input";
import { Select } from "@/ui/base/select";
import { Popover, PopoverTrigger, PopoverContent } from "@/ui/base/popover";
import { ViewModeToggle } from "@/ui/compositions/ViewModeToggle";
import { computePaginationRange } from "@/ui/compositions/paginationRange";
import type { ViewMode } from "@/shared/api/types-pages/pages";

export interface TableViewFilter {
  key: string;
  value: string;
  onChange: (val: string) => void;
  options: Array<{ value: string; label: string; tone?: "ok" | "warn" | "error" | undefined }>;
  placeholder?: string | undefined;
  /** "select" (default) renders a dropdown; "chips" renders an inline
   *  toggle-group so common statuses are one click away. */
  variant?: "select" | "chips" | undefined;
}

export interface TableViewColumn {
  key: string;
  label: string;
}

export interface TableViewSearchConfig {
  value: string;
  onChange: (val: string) => void;
  placeholder?: string | undefined;
}

export interface TableViewPaginationConfig {
  page: number;
  totalPages: number;
  totalItems?: number | undefined;
  pageSize?: number | undefined;
  onChange: (page: number) => void;
}

export interface TableViewViewModeConfig {
  current: ViewMode;
  onChange: (mode: Readonly<ViewMode>) => void;
}

export interface TableViewColumnsConfig {
  available: TableViewColumn[];
  visibility: Record<string, boolean>;
  onChange: (key: string, visible: boolean) => void;
}

export interface TableViewProps {
  search?: TableViewSearchConfig | undefined;
  filters?: TableViewFilter[] | undefined;
  viewMode?: TableViewViewModeConfig | undefined;
  columns?: TableViewColumnsConfig | undefined;
  pagination?: TableViewPaginationConfig | undefined;
  children: React.ReactNode;
  className?: string | undefined;
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
}: Readonly<TableViewProps>) {
  const { t } = useTranslation("pagination");
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
              filters?.map((f) =>
                f.variant === "chips" ? (
                  <div
                    key={f.key}
                    role="tablist"
                    aria-label={f.placeholder ?? "Filter"}
                    className="inline-flex items-center gap-0.5 p-0.5 rounded-xs border border-border-hi bg-bg overflow-x-auto"
                  >
                    {f.options.map((o) => {
                      const active = o.value === f.value;
                      const toneDot = (() => {
                        if (o.tone === "ok") return "bg-status-ok";
                        if (o.tone === "warn") return "bg-status-warn";
                        if (o.tone === "error") return "bg-status-error";
                        return "";
                      })();
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
              ? t("showing", {
                  start: rangeStart,
                  end: rangeEnd,
                  total: pagination.totalItems,
                })
              : t("pageOf", {
                  page: currentPage,
                  total: pagination?.totalPages ?? 0,
                })}
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
              aria-label={t("previous")}
            >
              <ChevronLeft className="size-4" />
            </button>

            {/* Collapsible page numbers — windowed around the current
                page with first/last anchors and ellipsis gaps so the
                control stays compact for large page counts. */}
            {computePaginationRange(currentPage, pagination?.totalPages ?? 0).map(
              (token, i) =>
                token === "ellipsis" ? (
                  <span
                    // Index-based key is stable here: the token sequence is
                    // positional and ellipses carry no identity of their own.
                    key={`ellipsis-${i}`}
                    aria-hidden="true"
                    className="flex items-center justify-center h-8 w-8 text-fg-muted font-mono text-xs"
                  >
                    …
                  </span>
                ) : (
                  <button
                    key={token}
                    onClick={() => pagination?.onChange(token)}
                    className={cn(
                      "flex items-center justify-center h-8 w-8 rounded-xs border font-mono text-xs transition-colors",
                      "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/50",
                      token === currentPage
                        ? "bg-accent border-accent text-white"
                        : "bg-bg-card border-border-hi text-fg-muted hover:text-fg",
                    )}
                    aria-label={t("page", { page: token })}
                    aria-current={token === currentPage ? "page" : undefined}
                  >
                    {token}
                  </button>
                ),
            )}

            <button
              onClick={() => pagination?.onChange(currentPage + 1)}
              disabled={currentPage >= (pagination?.totalPages ?? 0)}
              className={cn(
                "flex items-center justify-center h-8 w-8 rounded-xs border border-border-hi",
                "bg-bg-card text-fg-muted hover:text-fg transition-colors",
                "disabled:opacity-40 disabled:cursor-not-allowed",
                "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/50",
              )}
              aria-label={t("next")}
            >
              <ChevronRight className="size-4" />
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
