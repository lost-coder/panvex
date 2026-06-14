import { cn } from "@/ui/lib/cn";

export interface SettingsRowProps {
  label: React.ReactNode;
  description?: string;
  children: React.ReactNode;
  className?: string;
}

export function SettingsRow({ label, description, children, className }: Readonly<SettingsRowProps>) {
  return (
    // U-12: stack label/description over the control on mobile so neither the
    // (often long) config key/description gets truncated nor the input gets
    // shoved 200px to the right edge. From sm up, restore the side-by-side row.
    <div
      className={cn(
        "flex flex-col gap-2 px-4 py-3 sm:flex-row sm:items-center sm:justify-between sm:gap-4",
        className,
      )}
    >
      <div className="flex flex-col gap-0.5 min-w-0 sm:flex-1">
        {typeof label === "string" ? <span className="text-sm text-fg">{label}</span> : label}
        {description && <span className="text-caption leading-snug break-words">{description}</span>}
      </div>
      <div className="sm:shrink-0">{children}</div>
    </div>
  );
}
