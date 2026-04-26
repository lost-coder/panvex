import * as React from "react";
import { cn } from "@/ui/lib/cn";

export interface SelectOption {
  value: string;
  label: string;
}

export interface SelectProps {
  options: SelectOption[];
  value?: string | undefined;
  onChange?: ((value: string) => void) | undefined;
  placeholder?: string | undefined;
  disabled?: boolean | undefined;
  className?: string | undefined;
}

export const Select = React.forwardRef<HTMLSelectElement, SelectProps>(
  ({ options, value, onChange, placeholder = "Select…", disabled = false, className }, ref) => {
    return (
      <div className={cn("relative", className)}>
        <select
          ref={ref}
          value={value ?? ""}
          onChange={(e) => onChange?.(e.target.value)}
          disabled={disabled}
          className={cn(
            "flex h-10 w-full appearance-none rounded-xs border border-border-hi bg-bg-card pl-3 pr-8 py-2",
            "text-sm text-fg font-mono",
            "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/50",
            "disabled:cursor-not-allowed disabled:opacity-40",
            !value && "text-fg-muted/60",
          )}
        >
          {!value && <option value="">{placeholder}</option>}
          {options.map((opt) => (
            <option key={opt.value} value={opt.value}>
              {opt.label}
            </option>
          ))}
        </select>
        <span className="pointer-events-none absolute right-2.5 top-1/2 -translate-y-1/2 text-fg-muted text-xs">
          ▾
        </span>
      </div>
    );
  },
);
Select.displayName = "Select";
