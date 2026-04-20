import { cn } from "@/ui/lib/cn";

export interface ToggleProps {
  checked: boolean;
  onChange: (checked: boolean) => void;
  disabled?: boolean;
  size?: "sm" | "md";
  className?: string;
}

const trackSize = { sm: "h-5 w-9", md: "h-6 w-11" } as const;
const thumbSize = { sm: "h-3.5 w-3.5", md: "h-4 w-4" } as const;
const thumbTranslate = { sm: "translate-x-[18px]", md: "translate-x-[22px]" } as const;

export function Toggle({
  checked,
  onChange,
  disabled = false,
  size = "md",
  className,
}: ToggleProps) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      disabled={disabled}
      onClick={() => onChange(!checked)}
      className={cn(
        "relative inline-flex shrink-0 rounded-full transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/50",
        trackSize[size],
        checked ? "bg-accent" : "bg-fg-faint",
        disabled && "opacity-40 cursor-not-allowed",
        className,
      )}
    >
      <span
        className={cn(
          "pointer-events-none inline-block rounded-full bg-white shadow-sm transition-transform",
          thumbSize[size],
          "translate-x-1 translate-y-[3px]",
          checked && thumbTranslate[size],
        )}
      />
    </button>
  );
}
