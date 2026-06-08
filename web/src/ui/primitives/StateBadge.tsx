import { cn } from "@/ui/lib/cn";
import { StatusPill } from "@/ui/primitives/StatusPill";
import type { PillTone } from "@/ui/tokens/colors";

export interface StateBadgeProps {
  tone: PillTone;
  glyph: string;
  /** Already-translated label, shown on the pill for non-ok tones. */
  label: string;
  className?: string | undefined;
}

/**
 * Shared status badge: a quiet ✓-style glyph chip for the healthy (`ok`)
 * tone, a loud StatusPill for every problem/neutral tone. Used by both
 * NodeStateBadge (servers) and ClientStateBadge (clients) so a status
 * renders identically across entities.
 */
export function StateBadge({ tone, glyph, label, className }: Readonly<StateBadgeProps>) {
  if (tone === "ok") {
    return (
      <span
        aria-hidden="true"
        className={cn(
          "inline-flex h-5 w-5 items-center justify-center rounded-full bg-status-ok/15 text-status-ok text-micro font-bold shrink-0",
          className,
        )}
      >
        {glyph}
      </span>
    );
  }
  return <StatusPill tone={tone} glyph={glyph} label={label} className={className} />;
}
