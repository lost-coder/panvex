import { cn } from "@/lib/cn";
import { LayoutDashboard, Server, Settings, Users } from "lucide-react";
import { Link, useRouterState } from "@tanstack/react-router";

const NAV_ITEMS = [
  { to: "/", icon: LayoutDashboard, label: "Dashboard" },
  { to: "/servers", icon: Server, label: "Servers" },
  { to: "/clients", icon: Users, label: "Clients" },
  { to: "/settings", icon: Settings, label: "Settings" },
];

export function MobileNav({ className }: { className?: string }) {
  const { location } = useRouterState();
  return (
    <nav
      className={cn(
        "fixed bottom-0 left-0 right-0 bg-surface border-t border-border flex items-center justify-around z-20 backdrop-blur-[var(--blur)] h-16 pb-[env(safe-area-inset-bottom)]",
        className
      )}
    >
      {NAV_ITEMS.map(({ to, icon: Icon, label }) => {
        const active = location.pathname === to || (to !== "/" && location.pathname.startsWith(to));
        return (
          <Link
            key={to}
            to={to}
            className={cn(
              "flex flex-col items-center gap-0.5 py-2 px-3 text-text-3 text-[10px] font-semibold transition-colors",
              active && "text-accent-bright"
            )}
          >
            <Icon className="w-5 h-5" />
            <span>{label}</span>
          </Link>
        );
      })}
    </nav>
  );
}
