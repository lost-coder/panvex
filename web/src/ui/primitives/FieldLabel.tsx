import { cn } from "@/ui/lib/cn";

export interface FieldLabelProps {
  children: React.ReactNode;
  className?: string;
  size?: "sm" | "xs"; // sm = text-micro, xs = text-nano
}

export function FieldLabel({ children, className, size = "sm" }: Readonly<FieldLabelProps>) {
  return (
    <span
      className={cn(
        "text-fg-muted uppercase tracking-wider font-medium leading-none",
        size === "sm" ? "text-micro" : "text-nano",
        className,
      )}
    >
      {children}
    </span>
  );
}
