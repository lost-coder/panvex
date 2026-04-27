import { cn } from "@/ui/lib/cn";

export interface SettingsGroupProps {
  title: string;
  description?: string;
  children: React.ReactNode;
  className?: string;
}

export function SettingsGroup({ title, description, children, className }: Readonly<SettingsGroupProps>) {
  return (
    <section className={cn("flex flex-col gap-1", className)}>
      <div className="flex flex-col gap-0.5 mb-2">
        <h3 className="text-section">{title}</h3>
        {description && <p className="text-caption leading-snug">{description}</p>}
      </div>
      <div className="rounded-xs bg-bg-card border border-border divide-y divide-border">
        {children}
      </div>
    </section>
  );
}
