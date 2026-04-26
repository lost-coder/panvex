import { Button } from "@/ui/base/button";
import { cn } from "@/ui/lib/cn";

export interface BulkAction<T extends string = string> {
  id: T;
  label: string;
  /** `danger` adds red styling. Defaults to `default`. */
  variant?: "default" | "danger" | "ghost" | undefined;
  /** Disabled explicitly. `pending` already disables all actions. */
  disabled?: boolean | undefined;
}

export interface BulkActionBarProps<T extends string = string> {
  /** Number of selected items. The bar renders nothing when `count === 0`. */
  count: number;
  /** Singular label for one selection — defaults to "selected". */
  noun?: string | undefined;
  /** Secondary descriptor, shown as a muted hint next to the count. */
  hint?: string | undefined;
  /** Action buttons. Rendered right-aligned. */
  actions: BulkAction<T>[];
  /** Called when an action is clicked. Receives the action id. */
  onAction: (id: T) => void | Promise<void>;
  /** Called when the operator clears the selection (× button). */
  onClear: () => void;
  /** `true` while a bulk mutation is in-flight; disables all actions. */
  pending?: boolean | undefined;
  /** Inline error banner under the bar when a bulk action fails. */
  error?: string | undefined;
  className?: string | undefined;
}

/**
 * Sticky bar that appears only while at least one item is selected. Gives
 * operators the selected count plus a row of actions; clearing the selection
 * dismisses the bar.
 */
export function BulkActionBar<T extends string = string>({
  count,
  noun = "selected",
  hint,
  actions,
  onAction,
  onClear,
  pending,
  error,
  className,
}: BulkActionBarProps<T>) {
  if (count === 0) return null;

  return (
    <div className={cn("flex flex-col gap-2", className)}>
      <div className="sticky top-0 z-20 flex flex-wrap items-center gap-3 px-4 py-2 rounded-xs bg-bg-card border border-accent/40 shadow-sm">
        <span className="text-sm font-mono text-fg">
          {count.toLocaleString()} {noun}
        </span>
        {hint && (
          <span className="hidden sm:inline text-[11px] font-mono text-fg-muted">
            · {hint}
          </span>
        )}
        <div className="flex items-center gap-2 ml-auto">
          {actions.map((a) => (
            <Button
              key={a.id}
              size="sm"
              variant={a.variant === "danger" ? "danger" : a.variant === "ghost" ? "ghost" : "default"}
              disabled={pending || a.disabled}
              onClick={() => onAction(a.id)}
            >
              {a.label}
            </Button>
          ))}
          <Button
            size="sm"
            variant="ghost"
            onClick={onClear}
            disabled={pending}
            aria-label="Clear selection"
          >
            ✕
          </Button>
        </div>
      </div>
      {error && (
        <div className="rounded-xs border border-status-error/30 bg-status-error/10 px-3 py-2 text-xs font-mono text-status-error">
          {error}
        </div>
      )}
    </div>
  );
}
