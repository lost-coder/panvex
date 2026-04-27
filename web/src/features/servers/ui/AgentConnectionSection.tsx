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
          <span className="ml-auto text-[10px] font-mono text-fg-muted">
            last seen {data.lastSeenAt}
          </span>
        </div>
        <div className="flex items-baseline gap-2">
          <span className="text-lg font-mono font-semibold text-fg tabular-nums">
            {data.version}
          </span>
          {updateAvailable && (
            <Badge variant="accent">update: {data.latestAgentVersion}</Badge>
          )}
        </div>
        <div className="flex items-center justify-between gap-2 text-[11px] font-mono text-fg-muted">
          <span>fleet: {data.fleetGroup}</span>
        </div>
        {updateAvailable && data.onUpdate && (
          <Button size="sm" onClick={data.onUpdate} className="self-start">
            Update agent to {data.latestAgentVersion}
          </Button>
        )}
      </div>

      {/* Certificate — lifecycle + re-enrollment grant */}
      <div className="p-4 flex flex-col gap-3">
        <div className="flex items-center justify-between">
          <span className="text-sm font-semibold text-fg">Certificate</span>
          <span className={cn("text-xs font-mono font-semibold tabular-nums", certTone)}>
            {data.certificate.remainingDays}d left
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
        <div className="flex items-center justify-between text-[11px] font-mono text-fg-muted">
          <span>issued {data.certificate.issuedAt}</span>
          <span>expires {data.certificate.expiresAt}</span>
        </div>

        {data.recoveryGrant ? (
          <div className="flex items-center justify-between mt-auto">
            <div className="flex items-center gap-2">
              <Badge variant={data.recoveryGrant.status === "allowed" ? "ok" : "default"}>
                Recovery {data.recoveryGrant.status}
              </Badge>
              {data.recoveryGrant.status === "allowed" && (
                <span className="text-[10px] font-mono text-fg-muted">
                  expires {new Date(data.recoveryGrant.expiresAtUnix * 1000).toLocaleTimeString()}
                </span>
              )}
            </div>
            {data.recoveryGrant.status === "allowed" && (
              <Button variant="ghost" size="sm" onClick={onRevokeGrant}>
                Revoke
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
            Allow re-enrollment
          </Button>
        )}
      </div>
    </section>
  );
}
