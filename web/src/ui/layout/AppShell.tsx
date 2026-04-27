import { cn } from "@/ui/lib/cn";
import { Sidebar } from "./Sidebar";
import { BottomNav } from "./BottomNav";
import type { NavItem } from "./types";

export interface AppShellProps {
  navItems: NavItem[];
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
        className="md:ml-[60px] pb-16 md:pb-0 min-h-screen outline-none"
      >
        {children}
      </main>

      <BottomNav items={navItems} activeId={activeId} onNavigate={onNavigate} />
    </div>
  );
}
