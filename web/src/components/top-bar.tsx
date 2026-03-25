import { Avatar } from "@/components/ui/avatar";
import { Kbd } from "@/components/ui/kbd";
import { ThemeToggle } from "@/components/ui/theme-toggle";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Search } from "lucide-react";
import { useRouterState, Link } from "@tanstack/react-router";

const ROUTE_TITLES: Record<string, string> = {
  "/": "Dashboard",
  "/servers": "Servers",
  "/clients": "Clients",
  "/settings": "Settings",
  "/profile": "Profile",
};

export function TopBar() {
  const { location } = useRouterState();
  const title = ROUTE_TITLES[location.pathname] ?? "Panvex";

  return (
    <header
      className="bg-topbar border-b border-border flex items-center justify-between px-4 md:px-5 z-10 backdrop-blur-[var(--blur)] h-[var(--topbar-h)] sticky top-0"
    >
      <span className="text-[13px] font-semibold text-text-1">{title}</span>
      <div className="flex items-center gap-2">
        <button className="flex items-center gap-2 px-3 py-1.5 bg-card border border-border rounded-xs text-text-3 text-xs cursor-pointer hover:border-border-hover transition-all">
          <Search className="w-3.5 h-3.5" />
          <span>Search...</span>
          <Kbd>Ctrl K</Kbd>
        </button>
        <ThemeToggle />
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <button className="cursor-pointer">
              <Avatar name="User" />
            </button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem asChild>
              <Link to="/profile">Profile</Link>
            </DropdownMenuItem>
            <DropdownMenuItem>Log out</DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </header>
  );
}
