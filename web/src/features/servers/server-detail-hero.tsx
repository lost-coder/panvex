import { ArrowLeft } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import type { ServerDetailViewModel } from "./server-detail-view-model";

export function ServerDetailHero({
  header,
  onBack,
  onDiagnosticsRefreshAction,
  onRecoveryAction,
  onDetailBoostAction,
  diagnosticsRefreshActionLabel,
  recoveryActionLabel,
  detailBoostActionLabel,
  diagnosticsRefreshActionPending = false,
  recoveryActionPending = false,
  detailBoostActionPending = false,
}: {
  header: ServerDetailViewModel["header"];
  onBack: () => void;
  onDiagnosticsRefreshAction?: () => void;
  onRecoveryAction?: () => void;
  onDetailBoostAction?: () => void;
  diagnosticsRefreshActionLabel?: string;
  recoveryActionLabel?: string;
  detailBoostActionLabel?: string;
  diagnosticsRefreshActionPending?: boolean;
  recoveryActionPending?: boolean;
  detailBoostActionPending?: boolean;
}) {
  return (
    <section className="server-detail-hero server-detail-surface">
      <div className="server-detail-hero__toolbar">
        <button className="server-detail-hero__back" onClick={onBack} type="button">
          <ArrowLeft className="h-4 w-4" />
          Back to Servers
        </button>
        {onRecoveryAction ? (
          <button
            className="server-detail-hero__recovery-action"
            disabled={recoveryActionPending}
            onClick={onRecoveryAction}
            type="button"
          >
            {recoveryActionPending ? "Updating Recovery..." : recoveryActionLabel}
          </button>
        ) : null}
        {onDiagnosticsRefreshAction ? (
          <button
            className="server-detail-hero__recovery-action"
            disabled={diagnosticsRefreshActionPending}
            onClick={onDiagnosticsRefreshAction}
            type="button"
          >
            {diagnosticsRefreshActionPending ? "Refreshing Diagnostics..." : diagnosticsRefreshActionLabel}
          </button>
        ) : null}
        {onDetailBoostAction ? (
          <button
            className="server-detail-hero__recovery-action"
            disabled={detailBoostActionPending}
            onClick={onDetailBoostAction}
            type="button"
          >
            {detailBoostActionPending ? "Updating Boost..." : detailBoostActionLabel}
          </button>
        ) : null}
      </div>
      <div className="server-detail-hero__body">
        <div className="server-detail-hero__content">
          <div className="server-detail-hero__eyebrow">Latest reported snapshot</div>
          <div className="server-detail-hero__title-row">
            <h1 className="server-detail-hero__title">{header.nameText}</h1>
            <Badge dot variant={header.statusTone}>
              {header.statusText}
            </Badge>
          </div>
          <p className="server-detail-hero__summary">
            {header.reasonText}. Runtime status, DC coverage, upstream health, and recent events
            from the latest reported snapshot for this server.
          </p>
        </div>
        <dl className="server-detail-hero__meta">
          <MetaPill label="Group" value={header.groupText} />
          <MetaPill label="Version" value={header.versionText} />
          <MetaPill label="Last seen" value={header.lastSeenText} />
          <MetaPill label="Mode" value={header.readOnlyText} />
          <MetaPill label="Freshness" value={header.freshnessText} />
          <MetaPill label="Boost" value={header.detailBoostText} />
          <MetaPill label="Recovery" value={header.certificateRecoveryText} />
        </dl>
      </div>
    </section>
  );
}

function MetaPill({ label, value }: { label: string; value: string }) {
  return (
    <div className="server-detail-hero__meta-pill">
      <dt>{label}</dt>
      <dd>{value}</dd>
    </div>
  );
}
