import { cn } from "@/ui/lib/cn";

export type PageSectionTone = "default" | "danger" | "warn";

export interface PageSectionProps {
  /** Lucide icon or any React component taking `className`. */
  icon: React.ComponentType<{ className?: string }>;
  title: string;
  description?: string;
  /** Right-aligned pill in the header. Used for "Admin" gating, role, etc. */
  badge?: React.ReactNode;
  /** Coloured border around the card body. `danger` = red, `warn` = amber. */
  tone?: PageSectionTone;
  /** Optional className for the outer section. */
  className?: string;
  children: React.ReactNode;
}

const toneBorder: Record<PageSectionTone, string> = {
  default: "border-border",
  danger: "border-status-error/30",
  warn: "border-status-warn/30",
};

const toneIcon: Record<PageSectionTone, string> = {
  default: "text-fg-muted",
  danger: "text-status-error",
  warn: "text-status-warn",
};

/**
 * Form-section shell: `icon + h3 + optional badge` header with an optional
 * description line, followed by a bordered card that divides children with
 * horizontal rules. Used on Settings and Profile pages.
 */
export function PageSection({
  icon: Icon,
  title,
  description,
  badge,
  tone = "default",
  className,
  children,
}: Readonly<PageSectionProps>) {
  return (
    <section className={cn("flex flex-col gap-3", className)}>
      <header className="flex items-center gap-2">
        <Icon className={cn("h-4 w-4 shrink-0", toneIcon[tone])} aria-hidden />
        <h3 className="text-section">{title}</h3>
        {badge}
      </header>
      {description && (
        <p className="text-caption leading-snug -mt-1.5 ml-6">{description}</p>
      )}
      <div
        className={cn(
          "rounded-xs bg-bg-card border divide-y divide-border",
          toneBorder[tone],
        )}
      >
        {children}
      </div>
    </section>
  );
}
