import { cn } from "@/ui/lib/cn";
import type { ModeKind, Severity } from "@/shared/api/types-pages/pages";

export interface TransportBadgeProps {
  mode: ModeKind;
  healthy: number;
  total: number;
  severity: Severity;
  className?: string;
}

// Project tokens are status-ok/warn/error (see src/ui-kit.css). The plan
// asked for *-soft variants which don't exist in this codebase — we follow
// the established convention of opacity-suffixed Tailwind utilities used by
// AlertItem ("bg-status-X/15", "border-status-X/30"). The Severity union
// has no separate "success"/"warning"; "critical" and "bad" both map to
// the error tone.
const severityClass: Record<Severity, string> = {
  ok:       "bg-status-ok/15 text-status-ok border-status-ok/50",
  warn:     "bg-status-warn/15 text-status-warn border-status-warn/50",
  critical: "bg-status-error/15 text-status-error border-status-error/50",
  bad:      "bg-status-error/15 text-status-error border-status-error/50",
};

const modeLabel: Record<ModeKind, string> = {
  me:       "ME",
  direct:   "Direct",
  fallback: "Fallback",
  me_down:  "ME down",
};

export function TransportBadge({
  mode,
  healthy,
  total,
  severity,
  className,
}: Readonly<TransportBadgeProps>) {
  const label =
    mode === "me_down" ? modeLabel[mode] : `${modeLabel[mode]} ${healthy}/${total}`;
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1 px-2 py-0.5 rounded-xs border font-mono text-xs",
        severityClass[severity],
        className,
      )}
    >
      {label}
    </span>
  );
}
