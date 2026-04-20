import { cn } from "@/ui/lib/cn";

export interface FilterChipProps {
  active: boolean;
  onClick: () => void;
  children: React.ReactNode;
  /** Optional numeric count rendered faintly after the label. */
  count?: number;
  /** Optional title for the chip (tooltip / a11y). */
  title?: string;
}

/**
 * Phase-7 chip: uppercase mono label, optional count badge, accent fill when
 * active. Used for tab pills, status filters, and window selectors across
 * list pages.
 */
export function FilterChip({ active, onClick, children, count, title }: FilterChipProps) {
  return (
    <button
      type="button"
      onClick={onClick}
      title={title}
      className={cn(
        "whitespace-nowrap rounded-xs border px-2.5 py-1 text-xs font-mono uppercase tracking-wider transition-colors",
        active
          ? "border-accent/40 bg-accent/10 text-accent"
          : "border-border bg-bg-card text-fg-muted hover:text-fg",
      )}
    >
      {children}
      {typeof count === "number" && (
        <span className="ml-1 text-fg-faint tabular-nums">{count}</span>
      )}
    </button>
  );
}
