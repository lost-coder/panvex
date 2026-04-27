import { cn } from "@/ui/lib/cn";

export interface PageHeaderProps {
  title: string;
  subtitle?: string;
  trailing?: React.ReactNode;
  className?: string;
}

export function PageHeader({ title, subtitle, trailing, className }: Readonly<PageHeaderProps>) {
  return (
    <div
      className={cn(
        "flex items-start justify-between gap-4 px-4 pt-5 pb-4 md:px-8 md:pt-7 md:pb-5",
        "border-b border-border mb-4 md:mb-6",
        className,
      )}
    >
      <div className="flex flex-col gap-1">
        <h1 className="text-lg font-semibold text-fg md:text-xl tracking-tight">{title}</h1>
        {subtitle && <p className="text-xs text-fg-muted md:text-sm">{subtitle}</p>}
      </div>
      {trailing && <div className="shrink-0">{trailing}</div>}
    </div>
  );
}
