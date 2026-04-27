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
      className={cn(
        "hidden md:flex flex-col items-center fixed left-0 top-0 bottom-0 w-[60px] z-20",
        "bg-bg-card border-r border-border",
        className,
      )}
    >
      <div className="flex items-center justify-center h-[52px] w-full border-b border-border shrink-0">
        <span className="text-base font-mono font-bold text-accent">{brand.charAt(0)}</span>
      </div>

      <nav className="flex-1 flex flex-col items-center gap-1 py-3 w-full overflow-y-auto">
        {items.map((item) => (
          <div key={item.id} className="relative group">
            <button
              type="button"
              onClick={() => onNavigate?.(item.id)}
              className={cn(
                "flex items-center justify-center h-10 w-10 rounded-xs text-lg transition-colors",
                item.id === activeId
                  ? "bg-accent/10 text-accent"
                  : "text-fg-muted hover:text-fg hover:bg-bg-hover",
              )}
            >
              {item.icon}
            </button>
            <span className="absolute left-full ml-2 top-1/2 -translate-y-1/2 px-2.5 py-1 rounded-xs bg-bg-card-hi text-xs text-fg whitespace-nowrap opacity-0 pointer-events-none group-hover:opacity-100 transition-opacity shadow-lg border border-border-hi z-50">
              {item.label}
            </span>
          </div>
        ))}
      </nav>

      {onLogout && (
        <div className="py-3 border-t border-border flex flex-col items-center w-full">
          <div className="relative group">
            <button
              type="button"
              onClick={onLogout}
              className="flex items-center justify-center h-10 w-10 rounded-xs text-lg transition-colors text-fg-muted hover:text-status-error hover:bg-status-error/10"
            >
              <LogOut className="w-5 h-5" />
            </button>
            <span className="absolute left-full ml-2 top-1/2 -translate-y-1/2 px-2.5 py-1 rounded-xs bg-bg-card-hi text-xs text-fg whitespace-nowrap opacity-0 pointer-events-none group-hover:opacity-100 transition-opacity shadow-lg border border-border-hi z-50">
              Log out
            </span>
          </div>
        </div>
      )}

      {footer && (
        <div className="py-3 border-t border-border text-[10px] text-fg-muted text-center w-full">
          {footer}
        </div>
      )}
    </aside>
  );
}
