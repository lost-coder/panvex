// R-Q-08: Secret reveal/copy/rotate card extracted from
// ClientDetailPage.tsx. Owns its own reveal state — pages just supply
// the secret + rotate handler.

import { useState } from "react";

import { Button, CopyButton } from "@/ui";

export interface SecretSectionProps {
  secret: string;
  onRotate?: (() => void) | undefined;
  rotating?: boolean | undefined;
  pendingRedeploy?: boolean | undefined;
}

export function SecretSection({
  secret,
  onRotate,
  rotating,
  pendingRedeploy,
}: Readonly<SecretSectionProps>) {
  // Client secrets need a long-lived reveal/copy flow, not the one-shot
  // <SecretReveal> primitive used for TOTP bootstraps.
  const [revealed, setRevealed] = useState(false);
  // The Panvex API ships `secret` only on create + rotate responses
  // (omitempty on the regular GET). When it's absent we tell the
  // operator up front instead of showing a broken Reveal toggle —
  // tracked as backend follow-up #4.
  const hasSecret = !!secret;
  const masked = hasSecret ? "•".repeat(Math.min(32, Math.max(8, secret.length))) : "";
  return (
    <section className="rounded-xs bg-bg-card border border-divider p-4 flex flex-col gap-3">
      <div className="flex items-center justify-between gap-3 flex-wrap">
        <div className="flex items-baseline gap-2">
          <span className="text-sm font-semibold text-fg">Secret</span>
          <span className="text-[11px] font-mono text-fg-muted">
            rotating invalidates every outstanding connection link
          </span>
        </div>
        {onRotate && (
          <Button size="sm" variant="outline" disabled={rotating} onClick={onRotate}>
            {rotating ? "Rotating…" : "Rotate secret"}
          </Button>
        )}
      </div>
      {hasSecret ? (
        <div className="flex items-center gap-2 rounded-xs bg-bg border border-divider px-3 py-2 min-w-0">
          <code className="flex-1 min-w-0 text-sm font-mono text-fg break-all select-all">
            {revealed ? secret : masked}
          </code>
          <Button
            size="sm"
            variant="ghost"
            onClick={() => setRevealed((v) => !v)}
            className="shrink-0"
          >
            {revealed ? "Hide" : "Reveal"}
          </Button>
          <CopyButton text={secret} />
        </div>
      ) : (
        <div className="rounded-xs bg-bg border border-dashed border-divider px-3 py-2 text-[11px] font-mono text-fg-muted">
          Current secret isn't returned by the detail API — extract it from a
          fresh connection link below, or rotate to get a new one.
        </div>
      )}
      {pendingRedeploy && (
        <div className="text-[11px] font-mono text-status-warn">
          Secret rotated — wait for agents to re-apply before distributing new links.
        </div>
      )}
    </section>
  );
}
