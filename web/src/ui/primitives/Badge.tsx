import { cn } from "@/ui/lib/cn";
import { cva, type VariantProps } from "class-variance-authority";

const badgeVariants = cva(
  "inline-flex items-center rounded px-1.5 py-0.5 text-[10px] font-mono font-semibold uppercase leading-none tracking-wider whitespace-nowrap",
  {
    variants: {
      variant: {
        default: "bg-fg-faint text-fg-muted",
        ok: "bg-status-ok/15 text-status-ok",
        warn: "bg-status-warn/15 text-status-warn",
        error: "bg-status-error/15 text-status-error",
        accent: "bg-accent/15 text-accent",
      },
    },
    defaultVariants: { variant: "default" },
  },
);

export interface BadgeProps
  extends React.HTMLAttributes<HTMLSpanElement>, VariantProps<typeof badgeVariants> {}

export function Badge({ variant, className, ...props }: BadgeProps) {
  return <span className={cn(badgeVariants({ variant }), className)} {...props} />;
}
