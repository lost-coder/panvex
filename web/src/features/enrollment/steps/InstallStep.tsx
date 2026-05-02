import { useState } from "react";

import { cn } from "@/ui/lib/cn";
import { Button } from "@/ui/base/button";
import { CopyButton } from "@/ui/primitives/CopyButton";
import type { EnrollmentWizardProps } from "@/shared/api/types-pages/pages";

export function InstallStep({
  installCommand,
  advancedOptions,
  onInstallConfirm,
  onBack,
  tokenValue,
  tokenExpiresInSecs,
}: Readonly<EnrollmentWizardProps>) {
  const [showTroubleshooting, setShowTroubleshooting] = useState(false);
  const expiresMin = Math.round(tokenExpiresInSecs / 60);

  const requirements: Array<{
    label: string;
    detail?: string | undefined;
    tone?: "default" | "warn" | undefined;
  }> = [
    { label: "Linux host (amd64 / arm64)" },
    { label: "Root privileges (sudo)" },
    { label: "systemd service manager" },
    { label: "curl or wget for bootstrap" },
    { label: "Telemt (mtproto-proxy) running locally" },
    {
      // Highlighted in amber because Telemt ships with metrics OFF —
      // operators routinely miss this and then wonder why per-client
      // traffic / IP / quota counters stay empty.
      tone: "warn",
      label: "Enable Telemt metrics export (disabled by default)",
      detail: advancedOptions?.telemtMetricsUrl
        ? `agent will poll ${advancedOptions.telemtMetricsUrl}`
        : undefined,
    },
  ];

  return (
    <div className="flex flex-col gap-4">
      <div className="rounded-xs bg-bg-card border border-divider p-3">
        <div className="text-[10px] font-medium text-fg-muted uppercase tracking-wider mb-2">
          Before you run the command
        </div>
        <div className="flex flex-col gap-1.5 text-xs text-fg">
          {requirements.map((r) => (
            <div key={r.label} className="flex items-start gap-2">
              <span
                className={cn(
                  "mt-0.5",
                  r.tone === "warn" ? "text-status-warn" : "text-status-ok",
                )}
              >
                {r.tone === "warn" ? "!" : "✓"}
              </span>
              <div className="flex flex-col min-w-0">
                <span className={cn(r.tone === "warn" && "text-status-warn font-medium")}>
                  {r.label}
                </span>
                {r.detail && (
                  <span className="text-[11px] font-mono text-fg-muted">{r.detail}</span>
                )}
              </div>
            </div>
          ))}
        </div>
      </div>

      <div>
        <div className="flex justify-between items-center mb-1.5">
          <span className="text-[10px] font-medium text-fg-muted uppercase tracking-wider">
            Install command
          </span>
          <CopyButton text={installCommand} />
        </div>
        <pre className="rounded-xs bg-bg border border-border p-3 text-xs font-mono text-fg leading-relaxed whitespace-pre-wrap break-all overflow-x-auto">
          {installCommand}
        </pre>
      </div>

      <button
        type="button"
        onClick={() => setShowTroubleshooting((v) => !v)}
        className="text-xs text-fg-muted hover:text-fg text-left"
      >
        {showTroubleshooting ? "▾" : "▸"} Troubleshooting
      </button>
      {showTroubleshooting && (
        <div className="rounded-xs border border-divider p-3 flex flex-col gap-3 text-xs">
          <div>
            <div className="font-medium text-fg">Connection refused</div>
            <div className="text-fg-muted">
              Check Telemt is running:{" "}
              <code className="bg-black/30 px-1 rounded">
                curl http://127.0.0.1:9091/v1/health
              </code>
            </div>
          </div>
          <div>
            <div className="font-medium text-fg">Metrics empty after connect</div>
            <div className="text-fg-muted">
              Telemt ships with metrics off. Enable the metrics exporter in your Telemt
              config and confirm{" "}
              <code className="bg-black/30 px-1 rounded">
                curl {advancedOptions?.telemtMetricsUrl || "http://127.0.0.1:8081"}
              </code>{" "}
              answers before bootstrapping.
            </div>
          </div>
          <div>
            <div className="font-medium text-fg">Permission denied</div>
            <div className="text-fg-muted">
              Run with <code className="bg-black/30 px-1 rounded">sudo</code> — root is required
              for systemd.
            </div>
          </div>
          <div>
            <div className="font-medium text-fg">Token expired</div>
            <div className="text-fg-muted">
              Go back and generate a new token. Tokens are single-use and time-limited.
            </div>
          </div>
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

      <div className="flex gap-2">
        <Button variant="ghost" onClick={onBack}>
          ← Back
        </Button>
        <Button className="flex-1" onClick={onInstallConfirm}>
          I've run the command →
        </Button>
      </div>
    </div>
  );
}
