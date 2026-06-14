import { cn } from "@/ui/lib/cn";
import { statusBgClass } from "@/ui/lib/status";
import type { Status } from "@/ui/tokens/colors";

export interface StatusDotProps {
  status: Status;
  size?: "sm" | "md";
  animated?: boolean;
  className?: string;
}

const sizeMap = { sm: "h-2 w-2", md: "h-3 w-3" } as const;

export function StatusDot({ status, size = "sm", animated = false, className }: Readonly<StatusDotProps>) {
  return (
    <span
      className={cn(
        "inline-block rounded-full shrink-0",
        sizeMap[size],
        statusBgClass[status],
        animated && "animate-breathe",
        className,
      )}
      aria-hidden="true"
    />
  );
}
