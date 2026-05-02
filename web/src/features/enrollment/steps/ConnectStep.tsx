import { useEffect } from "react";

import { cn } from "@/ui/lib/cn";
import { Button } from "@/ui/base/button";
import type { EnrollmentWizardProps } from "@/shared/api/types-pages/pages";

export function ConnectStep({
  connectionStatus,
  connectedAgent,
  tokenValue,
  tokenExpiresInSecs,
  onViewDetails,
  onCancel,
}: Readonly<EnrollmentWizardProps>) {
  const allDone =
    connectionStatus.bootstrap === "done" &&
    connectionStatus.grpcConnect === "done" &&
    connectionStatus.firstData === "done";

  useEffect(() => {
    if (allDone && connectedAgent && onViewDetails) {
      const id = globalThis.setTimeout(() => onViewDetails(), 300);
      return () => globalThis.clearTimeout(id);
    }
    return undefined;
  }, [allDone, connectedAgent, onViewDetails]);

  const expiresMin = Math.round(tokenExpiresInSecs / 60);
  const stages: Array<{
    key: string;
    label: string;
    detail: string;
    state: "pending" | "waiting" | "done";
  }> = [
    {
      key: "bootstrap",
      label: "Bootstrap",
      detail: "Agent received enrollment certificate",
      state: connectionStatus.bootstrap,
    },
    {
      key: "grpcConnect",
      label: "Gateway connected",
      detail: "gRPC stream to control-plane established",
      state: connectionStatus.grpcConnect,
    },
    {
      key: "firstData",
      label: "First snapshot",
      detail: "Runtime telemetry received",
      state: connectionStatus.firstData,
    },
  ];

  return (
    <div className="flex flex-col gap-4">
      <div className="relative pl-5">
        <span aria-hidden="true" className="absolute top-1 bottom-1 left-[6px] w-px bg-divider" />
        {stages.map((s) => {
          const dotColor = (() => {
            if (s.state === "done") return "bg-status-ok";
            if (s.state === "waiting") return "bg-status-warn";
            return "bg-fg-faint";
          })();
          return (
            <div key={s.key} className="relative py-3 first:pt-1 last:pb-1">
              <span
                aria-hidden="true"
                className={cn(
                  "absolute -left-[12px] top-[14px] h-2 w-2 rounded-full z-10",
                  dotColor,
                )}
              />
              {s.state === "waiting" && (
                <span
                  aria-hidden="true"
                  className="absolute -left-[14px] top-[12px] h-3 w-3 rounded-full border-2 border-status-warn border-t-transparent animate-spin"
                />
              )}
              <div className="flex items-baseline gap-3">
                <span
                  className={cn(
                    "text-sm font-medium",
                    s.state === "pending" ? "text-fg-muted" : "text-fg",
                  )}
                >
                  {s.label}
                </span>
                <span className="text-[10px] font-mono uppercase tracking-wider text-fg-muted">
                  {s.state}
                </span>
              </div>
              <div className="text-[11px] font-mono text-fg-muted">{s.detail}</div>
            </div>
          );
        })}
      </div>

      {allDone && connectedAgent && (
        <div className="rounded-xs bg-status-ok/8 border border-status-ok/25 p-3 text-xs text-status-ok">
          <strong>{connectedAgent.id}</strong> is online. Redirecting to the server page…
        </div>
      )}

      <div className="flex items-center justify-between text-xs text-fg-muted rounded-xs bg-bg-card border border-divider px-3 py-2">
        <span>
          Token: <span className="font-mono">{tokenValue.slice(0, 12)}…</span>
        </span>
        <span>
          Expires in: <span className="text-status-warn">{expiresMin} min</span>
        </span>
      </div>

      <Button variant="ghost" onClick={onCancel}>
        Cancel
      </Button>
    </div>
  );
}
