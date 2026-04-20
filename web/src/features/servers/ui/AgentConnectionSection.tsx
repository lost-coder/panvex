// src/compositions/AgentConnectionSection.tsx
import { cn } from "@/ui/lib/cn";
import { presenceSeverity } from "@/ui/lib/status";
import { SectionHeader } from "@/ui/layout/SectionHeader";
import { Badge } from "@/ui/primitives/Badge";
import { StatusBeacon } from "@/ui/primitives/StatusBeacon";
import { MonoValue } from "@/ui/primitives/MonoValue";
import { FieldLabel } from "@/ui/primitives/FieldLabel";
import { KvGrid } from "@/ui/primitives/KvGrid";
import { Button } from "@/ui/base/button";
import type { AgentConnectionSectionProps } from "@/shared/api/types-pages/pages";

export function AgentConnectionSection({
  data,
  onAllowReEnrollment,
  onRevokeGrant,
}: AgentConnectionSectionProps) {
  const certColor =
    data.certificate.remainingDays > 7
      ? "text-status-ok"
      : data.certificate.remainingDays > 1
        ? "text-status-warn"
        : "text-status-error";

  const presenceStatus = presenceSeverity[data.presenceState] ?? "error";

  return (
    <div className="flex flex-col gap-4">
      <SectionHeader title="Agent Connection" />

      {/* Status */}
      <div className="rounded-xs bg-bg-card border border-border p-4 flex flex-col gap-4">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <StatusBeacon status={presenceStatus} />
            <span className="text-sm font-medium text-fg capitalize">{data.presenceState}</span>
          </div>
          <span className="text-xs text-fg-muted">Last seen: {data.lastSeenAt}</span>
        </div>

        <KvGrid
          rows={[
            { label: "Agent ID", value: <MonoValue>{data.agentId}</MonoValue> },
            {
              label: "Version",
              value: (
                <span className="inline-flex items-center gap-2">
                  <MonoValue>{data.version}</MonoValue>
                  {data.latestAgentVersion && data.latestAgentVersion !== data.version && (
                    <Badge variant="accent">Update available</Badge>
                  )}
                </span>
              ),
            },
            { label: "Fleet Group", value: data.fleetGroup },
          ]}
        />
        {data.latestAgentVersion && data.latestAgentVersion !== data.version && data.onUpdate && (
          <Button size="sm" onClick={data.onUpdate} className="self-start mt-1">
            Update to {data.latestAgentVersion}
          </Button>
        )}
      </div>

      {/* Certificate */}
      <div className="rounded-xs bg-bg-card border border-border p-4 flex flex-col gap-3">
        <FieldLabel>Certificate</FieldLabel>
        <KvGrid
          rows={[
            {
              label: "Issued",
              value: <span className="text-xs">{data.certificate.issuedAt}</span>,
            },
            {
              label: "Expires",
              value: <span className="text-xs">{data.certificate.expiresAt}</span>,
            },
            {
              label: "Remaining",
              value: (
                <span className={cn("text-xs font-medium", certColor)}>
                  {data.certificate.remainingDays} days
                </span>
              ),
            },
          ]}
        />

        {data.recoveryGrant ? (
          <div className="flex items-center justify-between mt-1">
            <div className="flex items-center gap-2">
              <Badge variant={data.recoveryGrant.status === "allowed" ? "ok" : "default"}>
                Recovery {data.recoveryGrant.status}
              </Badge>
              {data.recoveryGrant.status === "allowed" && (
                <span className="text-[10px] text-fg-muted">
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
            className="self-start mt-1"
          >
            Allow Re-enrollment
          </Button>
        )}
      </div>
    </div>
  );
}
