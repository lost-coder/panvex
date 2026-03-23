import { ReactNode } from "react";
import { ChevronDown } from "lucide-react";
import { cn } from "@/lib/cn";

interface FlipCardProps {
  front: ReactNode;
  back: ReactNode;
  expanded?: boolean;
  onToggle?: () => void;
}

export function FlipCard({ front, back, expanded = false, onToggle }: FlipCardProps) {
  return (
    <div className="cursor-pointer" onClick={onToggle}>
      <div
        className={cn(
          "rounded border border-border bg-card backdrop-blur-[var(--blur)] overflow-hidden transition-[border-color] duration-100 hover:border-border-hover",
          expanded && "border-accent/30"
        )}
      >
        <div className="relative">
          {front}
          <button
            type="button"
            onClick={(e) => { e.stopPropagation(); onToggle?.(); }}
            className="absolute top-2 right-2 text-text-4 hover:text-text-2 transition-colors"
            aria-label={expanded ? "Collapse" : "Expand"}
          >
            <ChevronDown
              className={cn(
                "w-4 h-4 transition-transform duration-200",
                expanded && "rotate-180"
              )}
            />
          </button>
        </div>
        {expanded && (
          <div className="bg-card-back border-t border-border">
            {back}
          </div>
        )}
      </div>
    </div>
  );
}
