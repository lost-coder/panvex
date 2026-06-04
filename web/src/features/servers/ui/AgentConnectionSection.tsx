import { useTranslation } from "react-i18next";

import { cn } from "@/ui/lib/cn";
import { presenceSeverity } from "@/ui/lib/status";
import { Badge } from "@/ui/primitives/Badge";
import { StatusBeacon } from "@/ui/primitives/StatusBeacon";
import { Button } from "@/ui/base/button";
import type { AgentConnectionSectionProps } from "@/shared/api/types-pages/pages";

/**
 * Compact agent-status card. The previous version stacked an
 * SectionHeader + two full cards + a KV grid for metadata the operator
 * rarely looks at (agent id, verbose last-seen strings). This revision
 * focuses on the two actionable things per review: the agent's running
 * version (with an inline update button when behind) and the TLS
 * certificate lifecycle (issued/expires/remaining + the re-enrollment
 * grant controls).
 */
export function AgentConnectionSection({
  data,
  onAllowReEnrollment,
  onRevokeGrant,
}: Readonly<AgentConnectionSectionProps>) {
  const { t } = useTranslation("servers");
  const certTone = (() => {
    if (data.certificate.remainingDays > 7) return "text-status-ok";
    if (data.certificate.remainingDays > 1) return "text-status-warn";
    return "text-status-error";
  })();
  const certBar = (() => {
    if (data.certificate.remainingDays > 7) return "bg-status-ok";
    if (data.certificate.remainingDays > 1) return "bg-status-warn";
    return "bg-status-error";
  })();
  const presenceStatus = presenceSeverity[data.presenceState] ?? "error";
  const updateAvailable =
    !!data.latestAgentVersion && data.latestAgentVersion !== data.version;

  return (
    <section className="rounded-xs bg-bg-card border border-divider grid grid-cols-1 md:grid-cols-2">
      {/* Agent — version + update CTA */}
      <div className="p-4 flex flex-col gap-3 border-b md:border-b-0 md:border-r border-divider">
        <div className="flex items-center gap-2">
          <StatusBeacon status={presenceStatus} />
          <span className="text-sm font-semibold text-fg capitalize">{data.presenceState}</span>
          <span className="ml-auto text-nano font-mono text-fg-muted">
            {t("detail.agentConnection.lastSeen", { value: data.lastSeenAt })}
          </span>
        </div>
        <div className="flex items-baseline gap-2">
          <span className="text-lg font-mono font-semibold text-fg tabular-nums">
            {data.version}
          </span>
          {updateAvailable && (
            <Badge variant="accent">
              {t("detail.agentConnection.update", { version: data.latestAgentVersion })}
            </Badge>
          )}
        </div>
        <div className="flex items-center justify-between gap-2 text-micro font-mono text-fg-muted">
          <span>{t("detail.agentConnection.fleet", { value: data.fleetGroup })}</span>
        </div>
        {updateAvailable && data.onUpdate && (
          <Button size="sm" onClick={data.onUpdate} className="self-start">
            {t("detail.agentConnection.updateAgent", { version: data.latestAgentVersion })}
          </Button>
        )}
      </div>

      {/* Certificate — lifecycle + re-enrollment grant */}
      <div className="p-4 flex flex-col gap-3">
        <div className="flex items-center justify-between">
          <span className="text-sm font-semibold text-fg">{t("detail.agentConnection.certificate")}</span>
          <span className={cn("text-xs font-mono font-semibold tabular-nums", certTone)}>
            {t("detail.agentConnection.daysLeft", { count: data.certificate.remainingDays })}
          </span>
        </div>
        <div className="h-1.5 w-full rounded-full bg-border overflow-hidden">
          <div
            className={cn("h-full rounded-full", certBar)}
            style={{
              width: `${Math.max(0, Math.min(100, (data.certificate.remainingDays / 30) * 100))}%`,
            }}
          />
        </div>
        <div className="flex items-center justify-between text-micro font-mono text-fg-muted">
          <span>{t("detail.agentConnection.issued", { value: data.certificate.issuedAt })}</span>
          <span>{t("detail.agentConnection.expires", { value: data.certificate.expiresAt })}</span>
        </div>

        {data.recoveryGrant ? (
          <div className="flex items-center justify-between mt-auto">
            <div className="flex items-center gap-2">
              <Badge variant={data.recoveryGrant.status === "allowed" ? "ok" : "default"}>
                {t("detail.agentConnection.recoveryStatus", { status: data.recoveryGrant.status })}
              </Badge>
              {data.recoveryGrant.status === "allowed" && (
                <span className="text-nano font-mono text-fg-muted">
                  {t("detail.agentConnection.recoveryExpires", {
                    value: new Date(data.recoveryGrant.expiresAtUnix * 1000).toLocaleTimeString(),
                  })}
                </span>
              )}
            </div>
            {data.recoveryGrant.status === "allowed" && (
              <Button variant="ghost" size="sm" onClick={onRevokeGrant}>
                {t("detail.agentConnection.revoke")}
              </Button>
            )}
          </div>
        ) : (
          <Button
            variant="ghost"
            size="sm"
            onClick={onAllowReEnrollment}
            className="self-start"
          >
            {t("detail.agentConnection.allowReEnrollment")}
          </Button>
        )}
      </div>
    </section>
  );
}
