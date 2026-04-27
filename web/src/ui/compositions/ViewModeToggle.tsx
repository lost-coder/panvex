import { cn } from "@/ui/lib/cn";
import type { ViewMode } from "@/shared/api/types-pages/pages";

export interface ViewModeToggleProps {
  mode: ViewMode;
  onChange: (mode: Readonly<ViewMode>) => void;
  className?: string;
}

export function ViewModeToggle({ mode, onChange, className }: Readonly<ViewModeToggleProps>) {
  return (
    <div className={cn("flex rounded-xs border border-border overflow-hidden", className)}>
      <button
        type="button"
        className={cn(
          "px-2.5 py-1.5 text-xs transition-colors",
          mode === "cards" ? "bg-accent text-white" : "bg-bg-card text-fg-muted hover:text-fg",
        )}
        onClick={() => onChange("cards")}
        aria-label="Card view"
      >
        <svg
          width="14"
          height="14"
          viewBox="0 0 14 14"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.5"
        >
          <rect x="1" y="1" width="5" height="5" rx="1" />
          <rect x="8" y="1" width="5" height="5" rx="1" />
          <rect x="1" y="8" width="5" height="5" rx="1" />
          <rect x="8" y="8" width="5" height="5" rx="1" />
        </svg>
      </button>
      <button
        type="button"
        className={cn(
          "px-2.5 py-1.5 text-xs transition-colors",
          mode === "list" ? "bg-accent text-white" : "bg-bg-card text-fg-muted hover:text-fg",
        )}
        onClick={() => onChange("list")}
        aria-label="List view"
      >
        <svg
          width="14"
          height="14"
          viewBox="0 0 14 14"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.5"
        >
          <line x1="1" y1="3" x2="13" y2="3" />
          <line x1="1" y1="7" x2="13" y2="7" />
          <line x1="1" y1="11" x2="13" y2="11" />
        </svg>
      </button>
    </div>
  );
}
