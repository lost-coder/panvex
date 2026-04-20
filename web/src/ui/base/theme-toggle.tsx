import { cn } from "@/ui/lib/cn";

export interface ThemeToggleProps {
  value?: "light" | "dark";
  onChange?: (value: "light" | "dark") => void;
  className?: string;
}

export function ThemeToggle({ value = "dark", onChange, className }: ThemeToggleProps) {
  const isLight = value === "light";

  return (
    <button
      type="button"
      onClick={() => onChange?.(isLight ? "dark" : "light")}
      className={cn(
        "h-8 px-2.5 rounded-xs text-xs font-mono transition-colors",
        "bg-bg-card border border-border-hi text-fg-muted hover:text-fg",
        className,
      )}
    >
      {isLight ? "☀ Light" : "☾ Dark"}
    </button>
  );
}
