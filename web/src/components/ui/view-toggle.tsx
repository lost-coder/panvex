import { cn } from "@/lib/cn";
import type { LucideIcon } from "lucide-react";

interface ViewToggleProps {
  views: Array<{ key: string; icon: LucideIcon }>;
  current: string;
  onChange: (key: string) => void;
}

export function ViewToggle({ views, current, onChange }: ViewToggleProps) {
  return (
    <div className="inline-flex border border-border rounded-xs overflow-hidden">
      {views.map((view, i) => {
        const Icon = view.icon;
        return (
          <button
            key={view.key}
            onClick={() => onChange(view.key)}
            className={cn(
              "p-2 transition-all",
              i > 0 && "border-l border-border",
              current === view.key
                ? "bg-accent-dim text-accent-bright"
                : "text-text-3 hover:text-text-1 hover:bg-input"
            )}
          >
            <Icon className="w-4 h-4" />
          </button>
        );
      })}
    </div>
  );
}
