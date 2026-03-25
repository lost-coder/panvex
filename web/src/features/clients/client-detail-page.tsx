import { useParams, useRouter } from "@tanstack/react-router";
import { ArrowLeft } from "lucide-react";
import { useClientDetail } from "./clients-state";
import { buildClientDetailViewModel } from "./client-detail-view-model";
import { ClientDetailAssignmentsPanel } from "./client-detail-assignments-panel";
import { ClientDetailDeploymentTable } from "./client-detail-deployment-table";
import { ClientDetailHero } from "./client-detail-hero";
import { ClientDetailIdentityPanel } from "./client-detail-identity-panel";
import { ClientDetailKpis } from "./client-detail-kpis";
import { ClientDetailLimitsPanel } from "./client-detail-limits-panel";
import { ClientDetailUsagePanel } from "./client-detail-usage-panel";

import "./client-detail.css";

export function ClientDetailPage() {
  const { clientId } = useParams({ strict: false }) as { clientId?: string };
  const router = useRouter();
  const { data: client, isLoading, isError } = useClientDetail(clientId ?? "");

  if (isLoading) {
    return (
      <div className="client-detail-page__state">
        <div className="h-8 w-48 rounded bg-surface animate-pulse" />
      </div>
    );
  }

  if (isError) {
    return (
      <div className="client-detail-page__state">
        <button
          className="client-detail-page__back-button"
          onClick={() => router.history.back()}
          type="button"
        >
          <ArrowLeft className="h-4 w-4" />
          Back to Clients
        </button>
        <p className="text-text-3">Client data is unavailable.</p>
      </div>
    );
  }

  if (!client) {
    return (
      <div className="client-detail-page__state">
        <button
          className="client-detail-page__back-button"
          onClick={() => router.history.back()}
          type="button"
        >
          <ArrowLeft className="h-4 w-4" />
          Back to Clients
        </button>
        <p className="text-text-3">Client not found.</p>
      </div>
    );
  }

  const viewModel = buildClientDetailViewModel(client);

  return (
    <div className="client-detail-page">
      <ClientDetailHero header={viewModel.header} onBack={() => router.history.back()} />
      <ClientDetailKpis stats={viewModel.overviewStats} />

      <div className="client-detail-page__secondary-grid">
        <section className="client-detail-section">
          <SectionHeading title="Identity & Secret" />
          <ClientDetailIdentityPanel
            items={viewModel.identityItems}
            panelKey={client.id}
            secret={viewModel.identitySecret}
          />
        </section>
        <section className="client-detail-section">
          <SectionHeading title="Usage" />
          <ClientDetailUsagePanel items={viewModel.usageItems} />
        </section>
      </div>

      <div className="client-detail-page__tertiary-grid">
        <section className="client-detail-section">
          <SectionHeading title="Limits" />
          <ClientDetailLimitsPanel items={viewModel.limitItems} />
        </section>
        <section className="client-detail-section">
          <SectionHeading title="Assignments" />
          <ClientDetailAssignmentsPanel
            agents={viewModel.assignmentAgents}
            groups={viewModel.assignmentGroups}
            summaryText={viewModel.assignmentSummaryText}
          />
        </section>
      </div>

      <section className="client-detail-section">
        <SectionHeading title="Deployment" />
        <ClientDetailDeploymentTable rows={viewModel.deploymentRows} />
      </section>
    </div>
  );
}

function SectionHeading({ title }: { title: string }) {
  return (
    <div className="client-detail-section-title">
      <span className="client-detail-section-title__label">{title}</span>
    </div>
  );
}
