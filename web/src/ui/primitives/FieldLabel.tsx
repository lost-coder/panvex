import { cn } from "@/ui/lib/cn";

export interface FieldLabelProps {
  children: React.ReactNode;
  className?: string;
  size?: "sm" | "xs"; // sm = text-[11px], xs = text-[10px]
}

export function FieldLabel({ children, className, size = "sm" }: FieldLabelProps) {
  return (
    <span
      className={cn(
        "text-fg-muted uppercase tracking-wider font-medium leading-none",
        size === "sm" ? "text-[11px]" : "text-[10px]",
        className,
      )}
    >
      {children}
    </span>
  );
}
