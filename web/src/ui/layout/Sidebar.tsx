import { LogOut, ChevronLeft, ChevronRight } from "lucide-react";
import { cn } from "@/ui/lib/cn";
import type { NavItem } from "./types";

export interface SidebarProps {
  items: NavItem[];
  activeId: string;
  brand?: string | undefined;
  /**
   * When true, the sidebar shows item labels next to icons and grows to
   * a full nav width. When false (default), it renders as a 64px icon
   * rail with hover/focus tooltips. Persistence is the caller's job —
   * AppShell stores the preference in localStorage.
   */
  expanded?: boolean | undefined;
  onToggleExpand?: (() => void) | undefined;
  footer?: React.ReactNode | undefined;
  onNavigate?: ((id: string) => void) | undefined;
  onLogout?: (() => void) | undefined;
  className?: string | undefined;
}

export function Sidebar({
  items,
  activeId,
  brand = "OPS",
  expanded = false,
  onToggleExpand,
  footer,
  onNavigate,
  onLogout,
  className,
}: Readonly<SidebarProps>) {
  // Active-bar offset must equal half of (rail width − button width):
  // - compact rail: (64 − 44) / 2 = 10px gap, so the marker sits at -10px
  //   from the button's left edge to land flush with the aside's edge.
  //   Anything bigger pushes it outside the rail and triggers a 1–2px
  //   horizontal scroll on the page.
  // - expanded rail: button is full-width, so the marker sits at left:0.

  return (
    <aside
      aria-label="Primary"
      className={cn(
        "hidden md:flex flex-col fixed left-0 top-0 bottom-0 z-20",
        "bg-bg-card border-r border-border transition-[width] duration-200",
        expanded ? "w-56 items-stretch" : "w-16 items-center",
        className,
      )}
    >
      <div
        className={cn(
          "relative flex items-center h-[52px] border-b border-border shrink-0 w-full",
          expanded ? "justify-between px-4" : "justify-center",
        )}
      >
        <span
          aria-hidden={expanded ? undefined : "true"}
          className="text-base font-mono font-bold text-accent whitespace-nowrap"
        >
          {expanded ? brand : brand.charAt(0)}
        </span>
        {!expanded && <span className="sr-only">{brand}</span>}
        {onToggleExpand && (
          <button
            type="button"
            aria-label={expanded ? "Collapse sidebar" : "Expand sidebar"}
            aria-expanded={expanded}
            onClick={onToggleExpand}
            className={cn(
              "flex items-center justify-center h-6 w-6 rounded-full text-fg-muted",
              "hover:text-fg hover:bg-bg-hover transition-colors",
              "focus-visible:outline-2 focus-visible:outline-accent focus-visible:outline-offset-1",
              expanded
                ? "shrink-0"
                : "absolute -right-3 top-3 bg-bg-card border border-border shadow-sm",
            )}
          >
            {expanded ? (
              <ChevronLeft className="w-4 h-4" />
            ) : (
              <ChevronRight className="w-4 h-4" />
            )}
          </button>
        )}
      </div>

      <nav
        aria-label="Primary"
        className={cn(
          "flex-1 flex flex-col gap-1 py-3 overflow-y-auto",
          "[scrollbar-width:none] [&::-webkit-scrollbar]:hidden",
          expanded ? "items-stretch px-2" : "items-center w-full",
        )}
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
                  "relative flex items-center rounded-xs transition-colors",
                  "focus-visible:outline-2 focus-visible:outline-accent focus-visible:outline-offset-1",
                  expanded
                    ? "w-full gap-3 h-11 px-3 text-sm"
                    : "justify-center h-11 w-11 text-lg",
                  isActive
                    ? cn(
                        "bg-accent/10 text-accent",
                        expanded
                          ? "before:absolute before:left-0 before:top-2 before:bottom-2 before:w-[2px] before:bg-accent before:rounded-r"
                          : "before:absolute before:-left-[10px] before:top-2 before:bottom-2 before:w-[2px] before:bg-accent before:rounded-r",
                      )
                    : "text-fg-muted hover:text-fg hover:bg-bg-hover",
                )}
              >
                <span className="shrink-0 flex items-center justify-center" aria-hidden="true">
                  {item.icon}
                </span>
                {expanded && (
                  <span className="truncate">{item.label}</span>
                )}
              </button>
              {!expanded && (
                <span
                  role="tooltip"
                  className="absolute left-full ml-2 top-1/2 -translate-y-1/2 px-2.5 py-1 rounded-xs bg-fg text-xs text-bg whitespace-nowrap opacity-0 pointer-events-none group-hover:opacity-100 group-focus-within:opacity-100 transition-opacity delay-100 shadow-xl z-50"
                >
                  {item.label}
                </span>
              )}
            </div>
          );
        })}
      </nav>

      {onLogout && (
        <div
          className={cn(
            "py-3 border-t border-border",
            expanded ? "px-2" : "w-full flex justify-center",
          )}
        >
          <div className="relative group">
            <button
              type="button"
              aria-label="Log out"
              onClick={onLogout}
              className={cn(
                "flex items-center rounded-xs transition-colors",
                "text-fg-muted hover:text-status-error hover:bg-status-error/10",
                "focus-visible:outline-2 focus-visible:outline-status-error focus-visible:outline-offset-1",
                expanded
                  ? "w-full gap-3 h-11 px-3 text-sm"
                  : "justify-center h-11 w-11 text-lg",
              )}
            >
              <LogOut className="w-5 h-5 shrink-0" />
              {expanded && <span>Log out</span>}
            </button>
            {!expanded && (
              <span
                role="tooltip"
                className="absolute left-full ml-2 top-1/2 -translate-y-1/2 px-2.5 py-1 rounded-xs bg-fg text-xs text-bg whitespace-nowrap opacity-0 pointer-events-none group-hover:opacity-100 group-focus-within:opacity-100 transition-opacity delay-100 shadow-xl z-50"
              >
                Log out
              </span>
            )}
          </div>
        </div>
      )}

      {footer && (
        <div
          className={cn(
            "py-3 border-t border-border text-[10px] text-fg-muted w-full",
            expanded ? "px-2" : "flex justify-center",
          )}
        >
          {footer}
        </div>
      )}
    </aside>
  );
}
