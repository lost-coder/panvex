import { cn } from "@/lib/cn";
import { LayoutDashboard, Server, Settings, Users } from "lucide-react";
import { Link, useRouterState } from "@tanstack/react-router";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";

const NAV_ITEMS = [
  { to: "/", icon: LayoutDashboard, label: "Dashboard" },
  { to: "/servers", icon: Server, label: "Servers" },
  { to: "/clients", icon: Users, label: "Clients" },
];

function NavItem({ to, icon: Icon, label }: { to: string; icon: React.ElementType; label: string }) {
  const { location } = useRouterState();
  const active = location.pathname === to || (to !== "/" && location.pathname.startsWith(to));
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Link
          to={to}
          className={cn(
            "relative p-3 rounded-xs text-text-3 hover:text-text-1 hover:bg-input transition-all cursor-pointer flex items-center justify-center",
            active && "text-accent-bright bg-accent-dim"
          )}
        >
          <Icon className="w-5 h-5" />
        </Link>
      </TooltipTrigger>
      <TooltipContent side="right">{label}</TooltipContent>
    </Tooltip>
  );
}

export function Rail({ className }: { className?: string }) {
  return (
    <aside
      style={{ gridArea: "rail" }}
      className={cn(
        "fixed top-0 left-0 bottom-0 w-[var(--rail-w)] bg-rail border-r border-border flex flex-col items-center py-3 gap-1 z-20 backdrop-blur-[var(--blur)]",
        className
      )}
    >
      <div className="w-8 h-8 rounded-xs bg-accent text-white font-extrabold text-sm flex items-center justify-center mb-4">
        P
      </div>
      <div className="flex flex-col items-center gap-1 flex-1">
        {NAV_ITEMS.map((item) => (
          <NavItem key={item.to} {...item} />
        ))}
      </div>
      <NavItem to="/settings" icon={Settings} label="Settings" />
    </aside>
  );
}
