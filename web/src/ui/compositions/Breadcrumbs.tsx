import { cn } from "@/ui/lib/cn";

export interface BreadcrumbItem {
  label: string;
  href?: string | undefined;
  onClick?: (() => void) | undefined;
}

export interface BreadcrumbsProps {
  items: BreadcrumbItem[];
  className?: string | undefined;
}

export function Breadcrumbs({ items, className }: BreadcrumbsProps) {
  const shouldCollapse = items.length > 2;

  return (
    <nav
      aria-label="Breadcrumb"
      className={cn("flex items-center gap-1.5 text-xs min-w-0", className)}
    >
      {items.map((item, i) => {
        const isLast = i === items.length - 1;
        const isFirst = i === 0;
        const isMiddle = !isFirst && !isLast;

        return (
          <span
            key={item.label}
            className={cn(
              "flex items-center gap-1.5 min-w-0",
              shouldCollapse && isMiddle && "hidden md:flex",
            )}
          >
            {i > 0 && <span className="text-fg-muted/40 shrink-0">/</span>}
            {isLast ? (
              <span className="text-fg font-medium truncate">{item.label}</span>
            ) : (
              <button
                type="button"
                onClick={item.onClick}
                className="text-fg-muted hover:text-fg transition-colors truncate"
              >
                {item.label}
              </button>
            )}
          </span>
        );
      })}
      {/* Mobile ellipsis for collapsed middle items */}
      {shouldCollapse && (
        <span className="flex items-center gap-1.5 md:hidden order-1">
          <span className="text-fg-muted/40">/</span>
          <span className="text-fg-muted">…</span>
        </span>
      )}
    </nav>
  );
}
