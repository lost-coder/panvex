// src/primitives/ChipToggle.tsx
import { cn } from "@/ui/lib/cn";

export interface ChipToggleProps {
  label: string;
  sublabel?: string;
  selected: boolean;
  onClick: () => void;
  className?: string;
}

export function ChipToggle({ label, sublabel, selected, onClick, className }: Readonly<ChipToggleProps>) {
  return (
    <button
      type="button"
      aria-pressed={selected}
      onClick={onClick}
      className={cn(
        "inline-flex items-center gap-1.5 px-3 py-1.5 rounded-full text-xs font-medium transition-colors",
        selected
          ? "bg-accent text-white"
          : "border border-border text-fg-muted hover:border-border-hi hover:text-fg",
        className,
      )}
    >
      {selected && (
        <span className="text-nano" aria-hidden="true">
          ✓
        </span>
      )}
      {label}
      {sublabel && (
        <span className={cn("text-nano", selected ? "opacity-70" : "text-fg-muted")}>
          {sublabel}
        </span>
      )}
    </button>
  );
}
