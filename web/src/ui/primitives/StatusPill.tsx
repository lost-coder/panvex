import { cn } from "@/ui/lib/cn";
import type { Status } from "@/ui/tokens/colors";

export interface StatusPillProps {
  tone: Status;
  /** Already-translated label, e.g. "DOWN". */
  label: string;
  /** Decorative shape glyph (color-blind safety). */
  glyph?: string | undefined;
  className?: string | undefined;
}

// Solid-fill pills: high-emphasis call-out for problem states. Text colors
// are chosen for AA contrast on each fill. The warn ink is a theme-aware token
// (text-status-warn-ink) so it clears AA on both the dark- and light-theme
// amber fills.
const toneClass: Record<Status, string> = {
  ok: "bg-status-ok/15 text-status-ok",
  warn: "bg-status-warn text-status-warn-ink",
  error: "bg-status-error text-white",
};

export function StatusPill({ tone, label, glyph, className }: Readonly<StatusPillProps>) {
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 rounded-md px-2 py-0.5 text-xs font-bold leading-none tracking-wide",
        toneClass[tone],
        className,
      )}
    >
      {glyph && (
        <span aria-hidden="true" className="text-[0.9em] leading-none">
          {glyph}
        </span>
      )}
      {label}
    </span>
  );
}
