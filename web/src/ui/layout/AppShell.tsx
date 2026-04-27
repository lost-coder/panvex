import { useEffect, useState } from "react";
import { cn } from "@/ui/lib/cn";
import { Sidebar } from "./Sidebar";
import { BottomNav } from "./BottomNav";
import type { NavItem } from "./types";

const SIDEBAR_PREF_KEY = "panvex.sidebarExpanded";

export interface AppShellProps {
  navItems: NavItem[];
  /**
   * Subset of nav items rendered in the mobile BottomNav. Defaults to
   * `navItems` when omitted. Material's bottom-nav-limit guideline caps
   * primary tabs at 5, so callers should pass at most 4 here when
   * `bottomNavMoreItems` is also provided (the 5th slot becomes "More").
   */
  bottomNavItems?: NavItem[];
  /**
   * Overflow nav items rendered in a "More" bottom sheet on mobile.
   * Sidebar continues to render the full `navItems` list since it has
   * the vertical room.
   */
  bottomNavMoreItems?: NavItem[];
  activeId: string;
  brand?: string;
  sidebarFooter?: React.ReactNode;
  onNavigate?: (id: string) => void;
  onLogout?: () => void;
  children: React.ReactNode;
  className?: string;
}

function readStoredExpanded(): boolean {
  if (typeof window === "undefined") return false;
  try {
    return window.localStorage.getItem(SIDEBAR_PREF_KEY) === "true";
  } catch {
    return false;
  }
}

export function AppShell({
  navItems,
  bottomNavItems,
  bottomNavMoreItems,
  activeId,
  brand,
  sidebarFooter,
  onNavigate,
  onLogout,
  children,
  className,
}: Readonly<AppShellProps>) {
  // Sidebar mode persists across reloads. Default is compact (icon-only
  // rail) so first-time visitors see the most content area; the toggle
  // is one click away in the brand header. Mobile (<md) hides the
  // sidebar entirely, so the preference only affects md+ layouts.
  const [expanded, setExpanded] = useState<boolean>(readStoredExpanded);

  useEffect(() => {
    if (typeof window === "undefined") return;
    try {
      window.localStorage.setItem(SIDEBAR_PREF_KEY, String(expanded));
    } catch {
      // Ignore quota / disabled-storage failures — the toggle still
      // works in-session, only the cross-reload persistence is lost.
    }
  }, [expanded]);

  return (
    <div className={cn("min-h-screen bg-bg overflow-x-hidden", className)}>
      <Sidebar
        items={navItems}
        activeId={activeId}
        brand={brand}
        expanded={expanded}
        onToggleExpand={() => setExpanded((v) => !v)}
        footer={sidebarFooter}
        onNavigate={onNavigate}
        onLogout={onLogout}
      />

      {/*
        P2-FE-08 / M-F7: `id="main-content"` is the target for the skip-nav
        link rendered in the host app's index.html. Keyboard users press
        Tab once, focus the skip link, activate it, and land on this
        landmark — bypassing the sidebar/nav on every page.
      */}
      {/*
        W6: `tabIndex={-1}` lets the router's post-navigation hook
        programmatically focus this landmark on every page change, so
        screen readers announce the new page instead of keeping focus
        trapped on the sidebar link the user just activated.
      */}
      <main
        id="main-content"
        tabIndex={-1}
        className={cn(
          "pb-16 md:pb-0 min-h-screen outline-none transition-[margin] duration-200",
          expanded ? "md:ml-56" : "md:ml-16",
        )}
      >
        {children}
      </main>

      <BottomNav
        items={bottomNavItems ?? navItems}
        moreItems={bottomNavMoreItems}
        activeId={activeId}
        onNavigate={onNavigate}
      />
    </div>
  );
}
