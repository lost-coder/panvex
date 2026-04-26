import { cn } from "@/ui/lib/cn";

export interface MonoValueProps {
  children: React.ReactNode;
  className?: string | undefined;
}

export function MonoValue({ children, className }: MonoValueProps) {
  return <span className={cn("font-mono text-xs text-fg", className)}>{children}</span>;
}
