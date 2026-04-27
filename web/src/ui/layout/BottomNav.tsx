import { useState } from "react";
import { MoreHorizontal } from "lucide-react";
import { cn } from "@/ui/lib/cn";
import { Sheet, SheetContent, SheetHeader, SheetTitle, SheetBody } from "@/ui/base/sheet";
import type { NavItem } from "./types";

export interface BottomNavProps {
  items: NavItem[];
  moreItems?: NavItem[] | undefined;
  activeId: string;
  onNavigate?: ((id: string) => void) | undefined;
  className?: string | undefined;
}

export function BottomNav({
  items,
  moreItems,
  activeId,
  onNavigate,
  className,
}: Readonly<BottomNavProps>) {
  const [moreOpen, setMoreOpen] = useState(false);
  const hasMore = !!moreItems && moreItems.length > 0;
  const moreActive = hasMore && moreItems!.some((m) => m.id === activeId);

  const handleNavigate = (id: string) => {
    setMoreOpen(false);
    onNavigate?.(id);
  };

  return (
    <>
      <nav
        aria-label="Primary"
        className={cn(
          "fixed bottom-0 left-0 right-0 z-30 flex md:hidden",
          "bg-bg-card border-t border-border",
          "pb-[env(safe-area-inset-bottom)]",
          className,
        )}
      >
        {items.map((item) => {
          const isActive = item.id === activeId;
          return (
            <button
              key={item.id}
              type="button"
              aria-label={item.label}
              aria-current={isActive ? "page" : undefined}
              onClick={() => handleNavigate(item.id)}
              className={cn(
                "flex-1 flex flex-col items-center gap-0.5 py-2 text-[10px]",
                "transition-all active:scale-[0.97]",
                "focus-visible:outline-2 focus-visible:outline-accent focus-visible:outline-offset-[-4px]",
                isActive ? "text-accent" : "text-fg-muted active:text-fg",
              )}
            >
              <span className="text-lg leading-none" aria-hidden="true">
                {item.icon}
              </span>
              <span>{item.label}</span>
            </button>
          );
        })}
        {hasMore && (
          <button
            type="button"
            aria-label="More navigation"
            aria-haspopup="dialog"
            aria-expanded={moreOpen}
            aria-current={moreActive ? "page" : undefined}
            onClick={() => setMoreOpen(true)}
            className={cn(
              "flex-1 flex flex-col items-center gap-0.5 py-2 text-[10px]",
              "transition-all active:scale-[0.97]",
              "focus-visible:outline-2 focus-visible:outline-accent focus-visible:outline-offset-[-4px]",
              moreActive ? "text-accent" : "text-fg-muted active:text-fg",
            )}
          >
            <span className="text-lg leading-none" aria-hidden="true">
              <MoreHorizontal size={20} />
            </span>
            <span>More</span>
          </button>
        )}
      </nav>

      {hasMore && (
        <Sheet open={moreOpen} onOpenChange={setMoreOpen}>
          <SheetContent side="bottom" className="md:hidden">
            <SheetHeader>
              <SheetTitle>More</SheetTitle>
            </SheetHeader>
            <SheetBody>
              <ul className="flex flex-col gap-0.5">
                {moreItems!.map((item) => {
                  const isActive = item.id === activeId;
                  return (
                    <li key={item.id}>
                      <button
                        type="button"
                        aria-label={item.label}
                        aria-current={isActive ? "page" : undefined}
                        onClick={() => handleNavigate(item.id)}
                        className={cn(
                          "w-full flex items-center gap-3 px-3 py-3 rounded-xs text-sm",
                          "transition-all active:scale-[0.99]",
                          "focus-visible:outline-2 focus-visible:outline-accent focus-visible:outline-offset-1",
                          isActive
                            ? "bg-accent/10 text-accent"
                            : "text-fg hover:bg-bg-hover",
                        )}
                      >
                        <span className="text-lg leading-none" aria-hidden="true">
                          {item.icon}
                        </span>
                        <span>{item.label}</span>
                      </button>
                    </li>
                  );
                })}
              </ul>
            </SheetBody>
          </SheetContent>
        </Sheet>
      )}
    </>
  );
}
