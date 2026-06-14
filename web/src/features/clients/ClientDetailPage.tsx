// Phase-7 redesign: hero band + pulse row + separate Secret section
// + combined Deployments & Links + GeoIP-ready IP history +
// always-visible Limits card.
//
// R-Q-08: every sub-section lives in `./components/` so this file is
// left with composition + the edit-form state machine.
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { ClientDetailHero } from "./components/ClientDetailHero";
import { ClientDetailPulse } from "./components/ClientDetailPulse";
import { ClientEditSheet } from "./components/ClientEditSheet";
import { DeployLinksCard } from "./components/DeployLinksCard";
import { IPHistoryCard } from "./components/IPHistoryCard";
import { LimitsCard } from "./components/LimitsCard";
import { ResetQuotaHistory } from "./components/ResetQuotaHistory";
import { SecretSection } from "./components/SecretSection";
import { SubscriptionLinkCard } from "./SubscriptionLinkCard";
import { deriveClientState } from "./components/ClientsPageCells";
import { useNowSec } from "@/shared/hooks/useNowSec";
import {
  Breadcrumbs,
  SwipeTabView,
  type ClientDetailPageProps,
  type ClientFormData,
} from "@/ui";

function formStateFromClient(client: ClientDetailPageProps["client"]): ClientFormData {
  return {
    name: client.name,
    userAdTag: client.userAdTag,
    // Edit opens with auto-generation OFF so the existing ad tag
    // (whatever it is, including empty) is preserved verbatim on
    // save. The operator can tick "Auto-generate" to request a new
    // tag when saving.
    userAdTagAuto: false,
    expirationRfc3339: client.expirationRfc3339,
    maxTcpConns: client.maxTcpConns,
    maxUniqueIps: client.maxUniqueIps,
    dataQuotaBytes: client.dataQuotaBytes,
    fleetGroupIds: [...client.fleetGroupIds],
    agentIds: [...client.agentIds],
  };
}

export function ClientDetailPage({
  client,
  onBack,
  onEdit,
  editLoading,
  editError,
  fleetGroups,
  agents,
  onRotateSecret,
  secretRotating,
  secretPendingRedeploy,
  onRotateSubscription,
  subscriptionRotating,
  onRedeploy,
  redeploying,
  onDisable,
  onDelete,
  ipHistory,
  agentLabels,
  onResetQuota,
  resetStates,
  onDismissResetState,
  onResetQuotaEverywhere,
  resetEverywherePending,
}: Readonly<ClientDetailPageProps>) {
  const { t } = useTranslation("clients");
  // Auto-refreshing "now" — lifted out of the render path so the
  // state derivation below stays pure (react-hooks/purity).
  const nowMs = useNowSec() * 1000;
  // Expose "Redeploy" as a prominent action whenever at least one
  // deployment is not yet succeeded — failed (Telemt rejected the
  // apply) or queued (agent offline / job in flight too long).
  // Without this, operators get stuck on a stale "queued" or "failed"
  // status with no way to retry short of editing fields.
  const hasFailedDeployment =
    onRedeploy !== undefined && client.deployments.some((d) => d.status !== "succeeded");
  const [editOpen, setEditOpen] = useState(false);
  const [editData, setEditData] = useState<ClientFormData>(() => formStateFromClient(client));
  // Reseed the form each time the sheet opens — editing a client whose
  // assignments were just changed elsewhere (e.g. fleet-group rename)
  // should start from the latest server snapshot, not a stale draft.
  const openEdit = () => {
    setEditData(formStateFromClient(client));
    setEditOpen(true);
  };

  // Plan 2h: unified 7-state badge. The detail payload lacks
  // assignedNodesCount / lastDeployStatus, so synthesise them from
  // deployments[] (assigned ≈ deployment count; deploy status mirrors
  // backend deriveClientDeployStatus).
  const lastDeployStatus = (() => {
    if (client.deployments.length === 0) return "idle";
    if (client.deployments.some((d) => d.status === "failed")) return "failed";
    if (client.deployments.some((d) => d.status !== "succeeded")) return "pending";
    return "succeeded";
  })();
  const clientState = deriveClientState(
    {
      enabled: client.enabled,
      expirationRfc3339: client.expirationRfc3339,
      trafficUsedBytes: client.trafficUsedBytes,
      dataQuotaBytes: client.dataQuotaBytes,
      assignedNodesCount: client.deployments.length,
      lastDeployStatus,
    },
    nowMs,
  );

  // Rotate confirmation is owned by the container (global ConfirmProvider
  // already wraps this flow with a `requireTypeMatch`-style dialog).
  // Page just forwards the click.
  const secretSection = (
    <SecretSection
      secret={client.secret}
      onRotate={onRotateSecret}
      rotating={secretRotating}
      pendingRedeploy={secretPendingRedeploy}
    />
  );
  const subscriptionCard = (
    <SubscriptionLinkCard
      url={client.subscriptionUrl}
      rotating={subscriptionRotating ?? false}
      onRotate={onRotateSubscription ?? (() => {})}
    />
  );
  const deployLinks = (
    <DeployLinksCard
      deployments={client.deployments}
      secretPendingRedeploy={secretPendingRedeploy}
      agentLabels={agentLabels}
      dataQuotaBytes={client.dataQuotaBytes}
      onResetQuota={onResetQuota}
      resetStates={resetStates}
      onDismissResetState={onDismissResetState}
    />
  );
  const ipHistoryCard = (
    <IPHistoryCard ips={ipHistory?.ips ?? []} totalUnique={ipHistory?.totalUnique ?? 0} />
  );
  const limitsCard = <LimitsCard client={client} />;
  // Phase 3: collapsed history of reset_quota audit events for this
  // client. Self-hides when there are zero rows so the section stays
  // out of the way until the operator actually resets.
  const resetHistoryCard = (
    <ResetQuotaHistory clientId={client.id} agentLabels={agentLabels} />
  );

  return (
    <>
      <div className="px-4 md:px-8 pt-3 pb-3">
        <Breadcrumbs items={[{ label: t("detail.breadcrumb"), onClick: onBack }, { label: client.name }]} />
      </div>

      <ClientDetailHero
        name={client.name}
        enabled={client.enabled}
        expirationRfc3339={client.expirationRfc3339}
        fleetGroupIds={client.fleetGroupIds}
        state={clientState}
        hasFailedDeployment={hasFailedDeployment}
        onEdit={onEdit ? openEdit : undefined}
        onDisable={onDisable}
        onRedeploy={onRedeploy}
        redeploying={redeploying}
        onDelete={onDelete}
        onResetQuotaEverywhere={
          // Hide the affordance for single-deployment clients —
          // there's no fan-out semantic and the per-row Reset already
          // covers the same intent.
          client.deployments.length >= 2 ? onResetQuotaEverywhere : undefined
        }
        resetEverywherePending={resetEverywherePending}
      />

      <div className="px-4 md:px-8 flex flex-col gap-6 pb-8 pt-6">
        <ClientDetailPulse client={client} />

        {/* Mobile: swipe tabs keep the scroll bounded on narrow viewports. */}
        <div className="md:hidden">
          {/* U-17: Deployments first — handing out a connection link is the
              primary task. Secret (a service action) moves last. */}
          <SwipeTabView
            tabs={[
              { id: "deploy", label: t("detail.tabs.deploy"), content: deployLinks },
              { id: "limits", label: t("detail.tabs.limits"), content: limitsCard },
              { id: "ips", label: t("detail.tabs.ips"), content: ipHistoryCard },
              { id: "secret", label: t("detail.tabs.secret"), content: secretSection },
              { id: "subscription", label: t("subscription.title"), content: subscriptionCard },
            ]}
          />
          {resetHistoryCard}
        </div>

        {/* Desktop: stacked sections, deployments-first to match the
            primary task (U-17); secret drops below the fold. */}
        <div className="hidden md:flex flex-col gap-5">
          {deployLinks}
          {limitsCard}
          {ipHistoryCard}
          {secretSection}
          {subscriptionCard}
          {resetHistoryCard}
        </div>
      </div>

      {onEdit && (
        <ClientEditSheet
          open={editOpen}
          data={editData}
          onChange={setEditData}
          onSubmit={async () => {
            await onEdit(editData);
            if (!editError) setEditOpen(false);
          }}
          onClose={() => setEditOpen(false)}
          loading={editLoading}
          error={editError}
          fleetGroups={fleetGroups}
          agents={agents}
        />
      )}
    </>
  );
}
