import { cn } from "@/ui/lib/cn";
import type { NavItem } from "./types";

export interface BottomNavProps {
  items: NavItem[];
  activeId: string;
  onNavigate?: ((id: string) => void) | undefined;
  className?: string | undefined;
}

export function BottomNav({ items, activeId, onNavigate, className }: Readonly<BottomNavProps>) {
  return (
    <nav
      className={cn(
        "fixed bottom-0 left-0 right-0 z-30 flex md:hidden",
        "bg-bg-card border-t border-border",
        "pb-[env(safe-area-inset-bottom)]",
        className,
      )}
    >
      {items.map((item) => (
        <button
          key={item.id}
          type="button"
          onClick={() => onNavigate?.(item.id)}
          className={cn(
            "flex-1 flex flex-col items-center gap-0.5 py-2 text-[10px] transition-colors",
            item.id === activeId ? "text-accent" : "text-fg-muted active:text-fg",
          )}
        >
          <span className="text-lg leading-none">{item.icon}</span>
          <span>{item.label}</span>
        </button>
      ))}
    </nav>
  );
}
