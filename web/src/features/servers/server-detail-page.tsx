import { useParams, useRouter } from "@tanstack/react-router";
import { ArrowLeft } from "lucide-react";
import { useAppearanceSettings } from "@/features/profile/profile-state";
import {
  useActivateTelemetryDetailBoost,
  useAllowAgentCertificateRecovery,
  useRefreshTelemetryDiagnostics,
  useRevokeAgentCertificateRecovery,
  useServerRecoveryAccess,
  useServerDetail,
} from "./servers-state";
import { buildServerDetailViewModel } from "./server-detail-view-model";
import { ServerDetailConnectionsPanel } from "./server-detail-connections-panel";
import { ServerDetailDcTable } from "./server-detail-dc-table";
import { ServerDetailDiagnosticsPanel } from "./server-detail-diagnostics-panel";
import { ServerDetailEventsPanel } from "./server-detail-events-panel";
import { ServerDetailHero } from "./server-detail-hero";
import { ServerDetailInitializationWatchPanel } from "./server-detail-initialization-watch-panel";
import { ServerDetailKpis } from "./server-detail-kpis";
import { ServerDetailMeDiagnosticsPanel } from "./server-detail-me-diagnostics-panel";
import { ServerDetailRuntimePanel } from "./server-detail-runtime-panel";
import { ServerDetailSecurityPanel } from "./server-detail-security-panel";
import { ServerDetailUpstreamsTable } from "./server-detail-upstreams-table";

import "./server-detail.css";

export function ServerDetailPage() {
  const { serverId } = useParams({ strict: false }) as { serverId?: string };
  const router = useRouter();
  const serverDetailQuery = useServerDetail(serverId ?? "");
  const appearanceQuery = useAppearanceSettings();
  const allowCertificateRecovery = useAllowAgentCertificateRecovery();
  const revokeCertificateRecovery = useRevokeAgentCertificateRecovery();
  const activateDetailBoost = useActivateTelemetryDetailBoost();
  const refreshDiagnostics = useRefreshTelemetryDiagnostics();
  const { canManageRecovery, canRefreshDiagnostics } = useServerRecoveryAccess();
  const summary = serverDetailQuery.data?.server;
  const agent = summary?.agent;

  if (serverDetailQuery.isLoading) {
    return (
      <div className="server-detail-page__state">
        <div className="h-8 w-48 rounded bg-surface animate-pulse" />
      </div>
    );
  }

  if (serverDetailQuery.isError) {
    return (
      <div className="server-detail-page__state">
        <button
          className="server-detail-page__back-button"
          onClick={() => router.history.back()}
          type="button"
        >
          <ArrowLeft className="h-4 w-4" />
          Back to Servers
        </button>
        <p className="text-text-3">Server data is unavailable.</p>
      </div>
    );
  }

  if (!agent) {
    return (
      <div className="server-detail-page__state">
        <button
          className="server-detail-page__back-button"
          onClick={() => router.history.back()}
          type="button"
        >
          <ArrowLeft className="h-4 w-4" />
          Back to Servers
        </button>
        <p className="text-text-3">Server not found.</p>
      </div>
    );
  }

  const viewModel = buildServerDetailViewModel(summary, {
    detail: serverDetailQuery.data,
  });
  const recoveryStatus = agent.certificate_recovery?.status;
  const showRecoveryAction = canManageRecovery;
  const isRecoveryAllowed = recoveryStatus === "allowed";
  const recoveryActionLabel = isRecoveryAllowed ? "Revoke Certificate Recovery" : "Allow Certificate Recovery";
  const recoveryActionPending = allowCertificateRecovery.isPending || revokeCertificateRecovery.isPending;
  const detailBoostActionLabel = summary.detail_boost.active ? "Extend Detail Boost" : "Start Detail Boost";
  const helpMode = appearanceQuery.data?.help_mode ?? "basic";
  const showHelp = helpMode !== "off";

  return (
    <div className="server-detail-page">
      <ServerDetailHero
        header={viewModel.header}
        diagnosticsRefreshActionLabel={canRefreshDiagnostics ? "Refresh Diagnostics" : undefined}
        diagnosticsRefreshActionPending={refreshDiagnostics.isPending}
        onDiagnosticsRefreshAction={canRefreshDiagnostics ? () => refreshDiagnostics.mutate({ agentID: agent.id }) : undefined}
        onDetailBoostAction={() => activateDetailBoost.mutate({ agentID: agent.id })}
        onRecoveryAction={showRecoveryAction ? () => {
          if (isRecoveryAllowed) {
            revokeCertificateRecovery.mutate({ agentID: agent.id });
            return;
          }
          allowCertificateRecovery.mutate({ agentID: agent.id, ttlSeconds: 900 });
        } : undefined}
        onBack={() => router.history.back()}
        detailBoostActionLabel={detailBoostActionLabel}
        detailBoostActionPending={activateDetailBoost.isPending}
        recoveryActionLabel={showRecoveryAction ? recoveryActionLabel : undefined}
        recoveryActionPending={recoveryActionPending}
      />
      <ServerDetailKpis stats={viewModel.overviewStats} />

      <section className="server-detail-section">
        <SectionHeading title="DC Health" />
        {showHelp ? <SectionHelp text="DC coverage, writers, and RTT show whether each Telegram data center currently has healthy routing capacity on this node." /> : null}
        <ServerDetailDcTable rows={viewModel.dcRows} />
      </section>

      {viewModel.initializationWatch.visible ? (
        <section className="server-detail-section">
          <SectionHeading title={viewModel.initializationWatch.titleText} />
          {showHelp ? <SectionHelp text="Initialization Watch stays visible while Telemt is starting and for a short cooldown after readiness so operators can confirm that startup really stabilized." /> : null}
          <ServerDetailInitializationWatchPanel watch={viewModel.initializationWatch} />
        </section>
      ) : null}

      <div className="server-detail-page__secondary-grid">
        <section className="server-detail-section">
          <SectionHeading title="Admission & Runtime Gates" />
          {showHelp ? <SectionHelp text="Runtime gates explain whether the node can accept new sessions right now and whether the ME runtime is still healthy after startup has already finished." /> : null}
          <ServerDetailRuntimePanel
            flags={viewModel.runtimeFlags}
          />
        </section>
        <section className="server-detail-section">
          <SectionHeading title="Connections" />
          {showHelp ? <SectionHelp text="Connection counters separate current live sockets from cumulative failures so operators can tell load problems from historical noise." /> : null}
          <ServerDetailConnectionsPanel
            meta={viewModel.connectionMeta}
            stats={viewModel.connectionStats}
          />
        </section>
      </div>

      <div className="server-detail-page__tertiary-grid">
        <section className="server-detail-section">
          <SectionHeading title="Upstreams" />
          {showHelp ? <SectionHelp text="Upstreams show the health of outbound routes Telemt can currently use. Healthy versus total is usually the fastest indicator of path instability." /> : null}
          <ServerDetailUpstreamsTable
            rows={viewModel.upstreamRows}
            summaryText={viewModel.upstreamSummaryText}
          />
        </section>
        <section className="server-detail-section">
          <SectionHeading title="Recent Events" />
          {showHelp ? <SectionHelp text="Recent events explain what changed most recently on the node and often reveal whether a degradation is still ongoing or already recovering." /> : null}
          <ServerDetailEventsPanel items={viewModel.recentEventItems} />
        </section>
      </div>

      <div className="server-detail-page__tertiary-grid">
        <section className="server-detail-section">
          <SectionHeading title="System & Limits" />
          {showHelp ? <SectionHelp text="System and limit diagnostics describe the Telemt build, config identity, and effective operational limits that influence runtime behavior on this node." /> : null}
          <ServerDetailDiagnosticsPanel
            helpMode={helpMode}
            rows={viewModel.diagnosticsRows}
            stateText={viewModel.diagnosticsStateText}
          />
        </section>
        <section className="server-detail-section">
          <SectionHeading title="Security & Whitelist" />
          {showHelp ? <SectionHelp text="Security posture explains whether the Telemt API is read-only, how requests are authenticated, and which whitelist entries currently gate API access." /> : null}
          <ServerDetailSecurityPanel
            entries={viewModel.whitelistEntries}
            helpMode={helpMode}
            rows={viewModel.securityRows}
            stateText={viewModel.securityStateText}
          />
        </section>
      </div>

      <section className="server-detail-section">
        <SectionHeading title="ME & Routing Diagnostics" />
        {showHelp ? <SectionHelp text="ME diagnostics show the current pool generations, while routing diagnostics expose the IP path Telemt currently selected per DC when minimal runtime snapshots are available." /> : null}
        <ServerDetailMeDiagnosticsPanel
          helpMode={helpMode}
          meRows={viewModel.meDiagnosticsRows}
          routingRows={viewModel.routingRows}
          stateText={viewModel.meDiagnosticsStateText}
        />
      </section>
    </div>
  );
}

function SectionHeading({ title }: { title: string }) {
  return (
    <div className="server-detail-section-title">
      <span className="server-detail-section-title__label">{title}</span>
    </div>
  );
}

function SectionHelp({ text }: { text: string }) {
  return <p className="mb-3 text-sm text-text-3">{text}</p>;
}
