import { cn } from "@/ui/lib/cn";
import { StatusLabel, type StatusTone } from "@/ui/primitives/StatusLabel";

export interface HeroMetaPill {
  label: string;
  value: string;
  /** If true, the value uses the monospace font. */
  mono?: boolean;
  /** Tone colour applied to the value. Default: inherit fg. */
  tone?: StatusTone;
}

const pillValueTone: Record<StatusTone, string> = {
  default: "text-fg",
  ok: "text-status-ok",
  warn: "text-status-warn",
  error: "text-status-error",
};

export interface HeroStripProps {
  /** Primary identifier — node name, client name, etc. */
  name: string;
  /** Status dot + label next to the name. Omit for static-state entities. */
  status?: { tone: StatusTone; label: string; animate?: boolean };
  /** Meta pills shown below the name. Rendered as "LABEL value" tuples. */
  pills?: HeroMetaPill[];
  /** Right-hand actions (dropdown, buttons, icons). */
  actions?: React.ReactNode;
  /** Optional prefix (e.g. a lucide icon) before the name. */
  prefix?: React.ReactNode;
  /** Adds a border-y so the strip reads as a full-bleed band. Defaults to true. */
  bleed?: boolean;
  className?: string;
}

/**
 * Full-bleed detail-page hero: name on the left with status + meta pills,
 * actions on the right. Used on Server detail and Client detail.
 *
 * Render it edge-to-edge inside the page wrapper (no horizontal padding
 * around it) — the `border-y` gives it that "band" feel.
 */
export function HeroStrip({
  name,
  status,
  pills,
  actions,
  prefix,
  bleed = true,
  className,
}: Readonly<HeroStripProps>) {
  return (
    <section
      className={cn(
        "flex flex-wrap items-center justify-between gap-4 bg-bg-card px-4 md:px-8 py-4",
        bleed && "border-y border-divider",
        className,
      )}
    >
      <div className="flex items-center gap-3 min-w-0">
        {prefix && <span className="shrink-0">{prefix}</span>}
        <div className="flex flex-col gap-1 min-w-0">
          <div className="flex items-center gap-2 flex-wrap">
            <h1 className="text-lg md:text-xl font-semibold tracking-tight text-fg truncate">
              {name}
            </h1>
            {status && (
              <StatusLabel
                tone={status.tone}
                label={status.label}
                animate={status.animate}
              />
            )}
          </div>
          {pills && pills.length > 0 && (
            <div className="flex flex-wrap items-center gap-3 text-[11px] font-mono text-fg-muted">
              {pills.map((p, i) => (
                <span key={`${p.label}-${i}`} className="inline-flex items-baseline gap-1">
                  <span className="uppercase tracking-wider text-fg-faint">
                    {p.label}
                  </span>
                  <span
                    className={cn(
                      p.mono && "font-mono",
                      p.tone ? pillValueTone[p.tone] : "text-fg",
                    )}
                  >
                    {p.value}
                  </span>
                </span>
              ))}
            </div>
          )}
        </div>
      </div>
      {actions && <div className="flex items-center gap-2 shrink-0">{actions}</div>}
    </section>
  );
}
