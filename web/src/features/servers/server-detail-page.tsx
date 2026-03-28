import { useParams, useRouter } from "@tanstack/react-router";
import { ArrowLeft } from "lucide-react";
import { useAllowAgentCertificateRecovery, useServers } from "./servers-state";
import { buildServerDetailViewModel } from "./server-detail-view-model";
import { ServerDetailConnectionsPanel } from "./server-detail-connections-panel";
import { ServerDetailDcTable } from "./server-detail-dc-table";
import { ServerDetailEventsPanel } from "./server-detail-events-panel";
import { ServerDetailHero } from "./server-detail-hero";
import { ServerDetailKpis } from "./server-detail-kpis";
import { ServerDetailRuntimePanel } from "./server-detail-runtime-panel";
import { ServerDetailUpstreamsTable } from "./server-detail-upstreams-table";

import "./server-detail.css";

export function ServerDetailPage() {
  const { serverId } = useParams({ strict: false }) as { serverId?: string };
  const router = useRouter();
  const { data: agents = [], isLoading, isError } = useServers();
  const allowCertificateRecovery = useAllowAgentCertificateRecovery();
  const agent = agents.find((candidate) => candidate.id === (serverId ?? ""));

  if (isLoading) {
    return (
      <div className="server-detail-page__state">
        <div className="h-8 w-48 rounded bg-surface animate-pulse" />
      </div>
    );
  }

  if (isError) {
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

  const viewModel = buildServerDetailViewModel(agent);

  return (
    <div className="server-detail-page">
      <ServerDetailHero
        allowCertificateRecoveryPending={allowCertificateRecovery.isPending}
        header={viewModel.header}
        onAllowCertificateRecovery={() => {
          allowCertificateRecovery.mutate({ agentID: agent.id, ttlSeconds: 900 });
        }}
        onBack={() => router.history.back()}
      />
      <ServerDetailKpis stats={viewModel.overviewStats} />

      <section className="server-detail-section">
        <SectionHeading title="DC Health" />
        <ServerDetailDcTable rows={viewModel.dcRows} />
      </section>

      <div className="server-detail-page__secondary-grid">
        <section className="server-detail-section">
          <SectionHeading title="Runtime State" />
          <ServerDetailRuntimePanel
            flags={viewModel.runtimeFlags}
            progressCards={viewModel.runtimeProgressCards}
          />
        </section>
        <section className="server-detail-section">
          <SectionHeading title="Connections" />
          <ServerDetailConnectionsPanel
            meta={viewModel.connectionMeta}
            stats={viewModel.connectionStats}
          />
        </section>
      </div>

      <div className="server-detail-page__tertiary-grid">
        <section className="server-detail-section">
          <SectionHeading title="Upstreams" />
          <ServerDetailUpstreamsTable
            rows={viewModel.upstreamRows}
            summaryText={viewModel.upstreamSummaryText}
          />
        </section>
        <section className="server-detail-section">
          <SectionHeading title="Recent Events" />
          <ServerDetailEventsPanel items={viewModel.recentEventItems} />
        </section>
      </div>
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
