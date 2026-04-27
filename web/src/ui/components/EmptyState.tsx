import { cn } from "@/ui/lib/cn";

export interface EmptyStateProps {
  icon?: string;
  title: string;
  description?: string;
  action?: React.ReactNode;
  className?: string;
}

export function EmptyState({ icon = "∅", title, description, action, className }: Readonly<EmptyStateProps>) {
  return (
    <div className={cn("flex flex-col items-center justify-center gap-3 py-12 px-4", className)}>
      <span className="text-3xl text-fg-faint">{icon}</span>
      <div className="flex flex-col items-center gap-1 text-center">
        <h3 className="text-sm font-medium text-fg-muted">{title}</h3>
        {description && <p className="text-xs text-fg-muted/70 max-w-[280px]">{description}</p>}
      </div>
      {action && <div className="mt-1">{action}</div>}
    </div>
  );
}
