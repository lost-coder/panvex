import { cn } from "@/ui/lib/cn";

export interface SectionHeaderProps {
  title: string;
  badge?: string | number;
  trailing?: React.ReactNode;
  className?: string;
}

export function SectionHeader({ title, badge, trailing, className }: Readonly<SectionHeaderProps>) {
  return (
    <div className={cn("flex items-center justify-between gap-3 mb-2", className)}>
      <div className="flex items-center gap-2">
        <h2 className="text-xs font-semibold text-fg-muted uppercase tracking-wider">{title}</h2>
        {badge !== undefined && (
          <span className="text-nano font-mono bg-fg-faint text-fg-muted rounded px-1.5 py-0.5 leading-none">
            {badge}
          </span>
        )}
      </div>
      {trailing && <div className="shrink-0">{trailing}</div>}
    </div>
  );
}
