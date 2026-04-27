import { LogOut } from "lucide-react";
import { cn } from "@/ui/lib/cn";
import type { NavItem } from "./types";

export interface SidebarProps {
  items: NavItem[];
  activeId: string;
  brand?: string | undefined;
  footer?: React.ReactNode | undefined;
  onNavigate?: ((id: string) => void) | undefined;
  onLogout?: (() => void) | undefined;
  className?: string | undefined;
}

export function Sidebar({
  items,
  activeId,
  brand = "OPS",
  footer,
  onNavigate,
  onLogout,
  className,
}: Readonly<SidebarProps>) {
  return (
    <aside
      aria-label="Primary"
      className={cn(
        "hidden md:flex flex-col items-center fixed left-0 top-0 bottom-0 w-16 z-20",
        "bg-bg-card border-r border-border",
        className,
      )}
    >
      <div className="flex items-center justify-center h-[52px] w-full border-b border-border shrink-0">
        <span aria-hidden="true" className="text-base font-mono font-bold text-accent">
          {brand.charAt(0)}
        </span>
        <span className="sr-only">{brand}</span>
      </div>

      <nav
        aria-label="Primary"
        className="flex-1 flex flex-col items-center gap-1 py-3 w-full overflow-y-auto [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
      >
        {items.map((item) => {
          const isActive = item.id === activeId;
          return (
            <div key={item.id} className="relative group">
              <button
                type="button"
                aria-label={item.label}
                aria-current={isActive ? "page" : undefined}
                onClick={() => onNavigate?.(item.id)}
                className={cn(
                  "relative flex items-center justify-center h-11 w-11 rounded-xs text-lg transition-colors",
                  "focus-visible:outline-2 focus-visible:outline-accent focus-visible:outline-offset-1",
                  isActive
                    ? "bg-accent/10 text-accent before:absolute before:-left-3 before:top-2 before:bottom-2 before:w-[2px] before:bg-accent before:rounded-r"
                    : "text-fg-muted hover:text-fg hover:bg-bg-hover",
                )}
              >
                {item.icon}
              </button>
              <span
                role="tooltip"
                className="absolute left-full ml-2 top-1/2 -translate-y-1/2 px-2.5 py-1 rounded-xs bg-bg-card-hi text-xs text-fg whitespace-nowrap opacity-0 pointer-events-none group-hover:opacity-100 group-focus-within:opacity-100 transition-opacity delay-100 shadow-lg border border-border-hi z-50"
              >
                {item.label}
              </span>
            </div>
          );
        })}
      </nav>

      {onLogout && (
        <div className="py-3 border-t border-border w-full flex justify-center">
          <div className="relative group">
            <button
              type="button"
              aria-label="Log out"
              onClick={onLogout}
              className={cn(
                "flex items-center justify-center h-11 w-11 rounded-xs text-lg transition-colors",
                "text-fg-muted hover:text-status-error hover:bg-status-error/10",
                "focus-visible:outline-2 focus-visible:outline-status-error focus-visible:outline-offset-1",
              )}
            >
              <LogOut className="w-5 h-5" />
            </button>
            <span
              role="tooltip"
              className="absolute left-full ml-2 top-1/2 -translate-y-1/2 px-2.5 py-1 rounded-xs bg-bg-card-hi text-xs text-fg whitespace-nowrap opacity-0 pointer-events-none group-hover:opacity-100 group-focus-within:opacity-100 transition-opacity delay-100 shadow-lg border border-border-hi z-50"
            >
              Log out
            </span>
          </div>
        </div>
      )}

      {footer && (
        <div className="py-3 border-t border-border text-[10px] text-fg-muted w-full flex justify-center">
          {footer}
        </div>
      )}
    </aside>
  );
}
