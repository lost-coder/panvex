import { useState } from "react";
import { useTranslation } from "react-i18next";

import { cn } from "@/ui/lib/cn";
import { Button } from "@/ui/base/button";
import { CopyButton } from "@/ui/primitives/CopyButton";
import type { EnrollmentWizardProps } from "@/shared/api/types-pages/pages";
import { TokenFooter } from "./TokenFooter";

export function InstallStep({
  installCommand,
  advancedOptions,
  onInstallConfirm,
  onBack,
  tokenValue,
  tokenExpiresInSecs,
  onGenerateToken,
}: Readonly<EnrollmentWizardProps>) {
  const { t } = useTranslation("enrollment");
  const [showTroubleshooting, setShowTroubleshooting] = useState(false);

  const requirements: Array<{
    label: string;
    detail?: string | undefined;
    tone?: "default" | "warn" | undefined;
  }> = [
    { label: t("installCommand.requirements.linux") },
    { label: t("installCommand.requirements.root") },
    { label: t("installCommand.requirements.systemd") },
    { label: t("installCommand.requirements.curl") },
    { label: t("installCommand.requirements.telemt") },
    {
      // Highlighted in amber because Telemt ships with metrics OFF —
      // operators routinely miss this and then wonder why per-client
      // traffic / IP / quota counters stay empty.
      tone: "warn",
      label: t("installCommand.requirements.metrics"),
      detail: advancedOptions?.telemtMetricsUrl
        ? t("installCommand.requirements.metricsDetail", {
            url: advancedOptions.telemtMetricsUrl,
          })
        : undefined,
    },
  ];

  const metricsUrl = advancedOptions?.telemtMetricsUrl || "http://127.0.0.1:8081";

  return (
    <div className="flex flex-col gap-4">
      <div className="rounded-xs bg-bg-card border border-divider p-3">
        <div className="text-nano font-medium text-fg-muted uppercase tracking-wider mb-2">
          {t("installCommand.requirementsHeading")}
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
                  <span className="text-micro font-mono text-fg-muted">{r.detail}</span>
                )}
              </div>
            </div>
          ))}
        </div>
      </div>

      <div>
        <div className="flex justify-between items-center mb-1.5">
          <span className="text-nano font-medium text-fg-muted uppercase tracking-wider">
            {t("installCommand.commandHeading")}
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
        {showTroubleshooting ? "▾" : "▸"} {t("installCommand.troubleshooting.toggle")}
      </button>
      {showTroubleshooting && (
        <div className="rounded-xs border border-divider p-3 flex flex-col gap-3 text-xs">
          <div>
            <div className="font-medium text-fg">
              {t("installCommand.troubleshooting.connectionRefused.title")}
            </div>
            <div className="text-fg-muted">
              {t("installCommand.troubleshooting.connectionRefused.body")}{" "}
              <code className="bg-black/30 px-1 rounded">
                {"curl http://127.0.0.1:9091/v1/health"}
              </code>
            </div>
          </div>
          <div>
            <div className="font-medium text-fg">
              {t("installCommand.troubleshooting.metricsEmpty.title")}
            </div>
            <div className="text-fg-muted">
              {t("installCommand.troubleshooting.metricsEmpty.bodyBefore")}{" "}
              <code className="bg-black/30 px-1 rounded">{`curl ${metricsUrl}`}</code>{" "}
              {t("installCommand.troubleshooting.metricsEmpty.bodyAfter")}
            </div>
          </div>
          <div>
            <div className="font-medium text-fg">
              {t("installCommand.troubleshooting.permissionDenied.title")}
            </div>
            <div className="text-fg-muted">
              {t("installCommand.troubleshooting.permissionDenied.bodyBefore")}{" "}
              <code className="bg-black/30 px-1 rounded">{"sudo"}</code>{" "}
              {t("installCommand.troubleshooting.permissionDenied.bodyAfter")}
            </div>
          </div>
          <div>
            <div className="font-medium text-fg">
              {t("installCommand.troubleshooting.tokenExpired.title")}
            </div>
            <div className="text-fg-muted">
              {t("installCommand.troubleshooting.tokenExpired.body")}
            </div>
          </div>
        </div>
      )}

      <TokenFooter
        tokenValue={tokenValue}
        remainingSecs={tokenExpiresInSecs}
        onRegenerate={onGenerateToken}
      />

      <div className="flex gap-2">
        <Button variant="ghost" onClick={onBack}>
          {t("installCommand.back")}
        </Button>
        <Button className="flex-1" onClick={onInstallConfirm}>
          {t("installCommand.confirm")}
        </Button>
      </div>
    </div>
  );
}
