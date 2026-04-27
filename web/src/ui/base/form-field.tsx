import React from "react";
import { cn } from "@/ui/lib/cn";

export interface FormFieldProps {
  label: string;
  description?: string;
  error?: string;
  htmlFor?: string;
  children: React.ReactNode;
  className?: string;
  /** Show asterisk after label */
  required?: boolean;
  /**
   * "default" — normal label (text-sm, sentence case)
   * "uppercase" — compact uppercase label used in sheets/wizards (text-xs, uppercase tracking-wider)
   * "compact" — smallest uppercase label used in dense grids (text-[10px])
   */
  variant?: "default" | "uppercase" | "compact";
}

const labelStyles = {
  default: "text-sm font-medium text-fg leading-none",
  uppercase: "text-xs font-medium text-fg-muted uppercase tracking-wider",
  compact: "text-[10px] font-medium text-fg-muted uppercase tracking-wider",
} as const;

export function FormField({
  label,
  description,
  error,
  htmlFor,
  children,
  className,
  required,
  variant = "default",
}: Readonly<FormFieldProps>) {
  const autoId = React.useId();
  const fieldId = htmlFor ?? autoId;

  return (
    <div className={cn("flex flex-col gap-1.5", className)}>
      <label htmlFor={fieldId} className={labelStyles[variant]}>
        {label}
        {required && " *"}
      </label>
      {description && !error && <p className="text-caption leading-snug">{description}</p>}
      {React.isValidElement(children)
        ? React.cloneElement(children as React.ReactElement<{ id?: string }>, { id: fieldId })
        : children}
      {error && <p className="text-caption text-status-error leading-snug">{error}</p>}
    </div>
  );
}
