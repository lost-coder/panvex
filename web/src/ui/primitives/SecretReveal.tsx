// src/primitives/SecretReveal.tsx
import { CopyButton } from "./CopyButton";
import { Button } from "@/ui/base/button";
import type { SecretRevealProps } from "@/shared/api/types-pages/pages";

export function SecretReveal({ secret, onDismiss }: Readonly<SecretRevealProps>) {
  return (
    <div className="flex flex-col gap-4">
      <div className="rounded-xs bg-status-warn/5 border border-status-warn/30 p-3 text-xs text-status-warn">
        This secret will not be shown again. Copy it now.
      </div>
      <div className="flex items-center gap-2 rounded-xs bg-bg-card border border-border px-3 py-2">
        <code className="flex-1 text-sm font-mono text-fg break-all select-all">{secret}</code>
        <CopyButton text={secret} />
      </div>
      <Button variant="default" size="sm" onClick={onDismiss} className="self-end">
        Done
      </Button>
    </div>
  );
}
