import * as React from "react";
import { Slot } from "@radix-ui/react-slot";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@/ui/lib/cn";

const buttonVariants = cva(
  "inline-flex items-center justify-center gap-2 whitespace-nowrap rounded-xs text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/50 disabled:pointer-events-none disabled:opacity-40",
  {
    variants: {
      variant: {
        default: "bg-accent text-white hover:bg-accent/80",
        danger: "bg-status-error text-white hover:bg-status-error/80",
        ghost: "text-fg-muted hover:bg-bg-hover hover:text-fg",
        outline:
          "border border-border-hi bg-transparent text-fg-muted hover:bg-bg-hover hover:text-fg",
      },
      size: {
        sm: "h-8 px-3 text-xs",
        default: "h-10 px-4",
        lg: "h-12 px-6 text-base",
      },
    },
    defaultVariants: {
      variant: "default",
      size: "default",
    },
  },
);

export interface ButtonProps
  extends React.ButtonHTMLAttributes<HTMLButtonElement>, VariantProps<typeof buttonVariants> {
  asChild?: boolean;
}

const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant, size, asChild = false, type, ...props }, ref) => {
    const Comp = asChild ? Slot : "button";
    // Default `type="button"` so a <Button> inside a <form> does not
    // accidentally submit. Native <button> elements default to "submit",
    // which has caused unintended form POSTs across Panvex forms. Callers
    // who need submit behaviour pass `type="submit"` explicitly.
    // When `asChild` is true the rendered element may not be a <button>,
    // so forwarding `type` would add an invalid HTML attribute; we only
    // apply the default when rendering a native <button>.
    const resolvedType = asChild ? type : (type ?? "button");
    return (
      <Comp
        className={cn(buttonVariants({ variant, size, className }))}
        ref={ref}
        type={resolvedType}
        {...props}
      />
    );
  },
);
Button.displayName = "Button";

export { Button, buttonVariants };
