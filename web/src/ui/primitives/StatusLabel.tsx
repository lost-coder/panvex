import { cn } from "@/ui/lib/cn";

export type StatusTone = "ok" | "warn" | "error" | "default";

export interface StatusLabelProps {
  /** Small tone-coloured dot + uppercase mono label. */
  tone: StatusTone;
  /** Text to show next to the dot. */
  label: string;
  /** Add a breathing pulse to the dot — used for "running" / "waiting". */
  animate?: boolean;
  className?: string;
}

const dotClass: Record<StatusTone, string> = {
  ok: "bg-status-ok",
  warn: "bg-status-warn",
  error: "bg-status-error",
  default: "bg-fg-faint",
};

const textClass: Record<StatusTone, string> = {
  ok: "text-status-ok",
  warn: "text-status-warn",
  error: "text-status-error",
  default: "text-fg-muted",
};

/**
 * Tiny status indicator: a coloured dot next to an uppercase mono label.
 *
 * Exists because the raw "dot + label" pattern had been dupe-written four
 * times (Activity job-status, TokenList, UsersManagement 2FA, Clients row).
 * Prefer this over hand-rolling an inline span — the tone mapping is
 * authoritative.
 */
export function StatusLabel({ tone, label, animate, className }: StatusLabelProps) {
  return (
    <span className={cn("inline-flex items-center gap-1.5", className)}>
      <span
        className={cn(
          "h-1.5 w-1.5 rounded-full shrink-0",
          dotClass[tone],
          animate && "animate-pulse",
        )}
        aria-hidden
      />
      <span
        className={cn(
          "text-[11px] font-mono uppercase tracking-wider",
          textClass[tone],
        )}
      >
        {label}
      </span>
    </span>
  );
}
