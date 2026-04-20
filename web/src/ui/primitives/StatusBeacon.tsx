import { cn } from "@/ui/lib/cn";
import type { Status } from "@/ui/tokens/colors";

export interface StatusBeaconProps {
  status: Status;
  size?: "xs" | "sm" | "md" | "lg";
  animated?: boolean;
  className?: string;
}

const sizeMap = {
  xs: "h-3 w-3",
  sm: "h-4 w-4",
  md: "h-6 w-6",
  lg: "h-8 w-8",
} as const;

const colorMap = {
  ok: "text-status-ok",
  warn: "text-status-warn",
  error: "text-status-error",
} as const;

export function StatusBeacon({
  status,
  size = "md",
  animated = true,
  className,
}: StatusBeaconProps) {
  return (
    <span
      className={cn(
        "inline-block rounded-full shrink-0",
        sizeMap[size],
        colorMap[status],
        "shadow-[0_0_8px_2px_currentColor]",
        animated && "animate-beacon-glow",
        className,
      )}
      style={{ backgroundColor: "currentColor" }}
      role="img"
      aria-label={`Beacon: ${status}`}
    />
  );
}
