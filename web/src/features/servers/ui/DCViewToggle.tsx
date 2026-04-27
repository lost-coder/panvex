import { useState } from "react";
import { cn } from "@/ui/lib/cn";

export interface DCViewToggleProps {
  cardsView: React.ReactNode;
  gridView: React.ReactNode;
  className?: string;
}

export function DCViewToggle({ cardsView, gridView, className }: Readonly<DCViewToggleProps>) {
  const [mode, setMode] = useState<"cards" | "grid">("cards");

  return (
    <div className={cn("flex flex-col gap-2", className)}>
      <div className="flex items-center gap-1 self-end">
        <ToggleBtn active={mode === "cards"} onClick={() => setMode("cards")}>
          ▤
        </ToggleBtn>
        <ToggleBtn active={mode === "grid"} onClick={() => setMode("grid")}>
          ⊞
        </ToggleBtn>
      </div>
      {mode === "cards" ? cardsView : gridView}
    </div>
  );
}

function ToggleBtn({
  active,
  onClick,
  children,
}: Readonly<{
  active: boolean;
  onClick: () => void;
  children: React.ReactNode;
}>) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      className={cn(
        "h-7 w-7 flex items-center justify-center rounded text-xs transition-colors",
        active ? "bg-accent/20 text-accent" : "text-fg-muted hover:text-fg hover:bg-bg-hover",
      )}
    >
      {children}
    </button>
  );
}
