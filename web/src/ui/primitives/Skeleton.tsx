import { cn } from "@/ui/lib/cn";

export interface SkeletonProps {
  className?: string;
}

export function Skeleton({ className }: SkeletonProps) {
  return <div className={cn("animate-pulse rounded-xs bg-fg-faint", className)} />;
}
