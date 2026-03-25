import * as React from "react"
import { cva, type VariantProps } from "class-variance-authority"
import { Slot } from "radix-ui"

import { cn } from "@/lib/cn"

const buttonVariants = cva(
  "inline-flex shrink-0 items-center justify-center gap-2 rounded-xs font-sans whitespace-nowrap transition-all duration-[var(--transition)] outline-none focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-accent disabled:pointer-events-none disabled:opacity-50 [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
  {
    variants: {
      variant: {
        default: "bg-accent text-white hover:bg-accent-bright shadow-none",
        destructive: "bg-bad text-white hover:opacity-90",
        outline: "bg-input text-text-2 border border-border hover:bg-accent-dim",
        secondary: "bg-input text-text-2 border border-border hover:bg-accent-dim",
        ghost: "bg-transparent text-text-2 hover:bg-input",
        link: "text-accent underline-offset-4 hover:underline",
      },
      size: {
        default: "px-4 py-2 text-[13px] font-semibold",
        xs: "h-6 gap-1 rounded-xs px-2 text-xs has-[>svg]:px-1.5 [&_svg:not([class*='size-'])]:size-3",
        sm: "px-3 py-1.5 text-xs font-semibold",
        lg: "px-6 py-3 text-sm font-semibold",
        icon: "size-9",
        "icon-xs": "size-6 rounded-xs [&_svg:not([class*='size-'])]:size-3",
        "icon-sm": "size-8",
        "icon-lg": "size-10",
      },
    },
    defaultVariants: {
      variant: "default",
      size: "default",
    },
  }
)

function Button({
  className,
  variant = "default",
  size = "default",
  asChild = false,
  ...props
}: React.ComponentProps<"button"> &
  VariantProps<typeof buttonVariants> & {
    asChild?: boolean
  }) {
  const Comp = asChild ? Slot.Root : "button"

  return (
    <Comp
      data-slot="button"
      data-variant={variant}
      data-size={size}
      className={cn(buttonVariants({ variant, size, className }))}
      {...props}
    />
  )
}

export { Button, buttonVariants }
