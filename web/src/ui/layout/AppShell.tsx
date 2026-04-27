import { cn } from "@/ui/lib/cn";
import { Sidebar } from "./Sidebar";
import { BottomNav } from "./BottomNav";
import type { NavItem } from "./types";

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
  return (
    <div className={cn("min-h-screen bg-bg", className)}>
      <Sidebar
        items={navItems}
        activeId={activeId}
        brand={brand}
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
        trapped on the sidebar link the user just activated. The
        landmark stays keyboard-neutral for mouse users (no visible
        focus ring because the hook calls focus({preventScroll:true})
        and focus-visible is not triggered by scriptable focus).
      */}
      <main
        id="main-content"
        tabIndex={-1}
        className="md:ml-16 pb-16 md:pb-0 min-h-screen outline-none"
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
