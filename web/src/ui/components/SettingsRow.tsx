import { cn } from "@/ui/lib/cn";

export interface SettingsRowProps {
  label: string;
  description?: string;
  children: React.ReactNode;
  className?: string;
}

export function SettingsRow({ label, description, children, className }: SettingsRowProps) {
  return (
    <div className={cn("flex items-center justify-between gap-4 px-4 py-3", className)}>
      <div className="flex flex-col gap-0.5 min-w-0 flex-1">
        <span className="text-sm text-fg">{label}</span>
        {description && <span className="text-caption leading-snug truncate">{description}</span>}
      </div>
      <div className="shrink-0">{children}</div>
    </div>
  );
}
