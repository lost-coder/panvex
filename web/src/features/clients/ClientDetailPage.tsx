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
import { SecretSection } from "./components/SecretSection";
import { clientStatus } from "./components/clientDetailHelpers";
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
  onRedeploy,
  redeploying,
  onDisable,
  onDelete,
  ipHistory,
  agentLabels,
}: Readonly<ClientDetailPageProps>) {
  const { t } = useTranslation("clients");
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

  const status = clientStatus(client.enabled, client.expirationRfc3339);
  const statusLabel = (() => {
    if (status === "expired") return t("detail.statusExpired");
    if (status === "disabled") return t("detail.statusDisabled");
    return t("detail.statusActive");
  })();

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
  const deployLinks = (
    <DeployLinksCard
      deployments={client.deployments}
      secretPendingRedeploy={secretPendingRedeploy}
      agentLabels={agentLabels}
    />
  );
  const ipHistoryCard = (
    <IPHistoryCard ips={ipHistory?.ips ?? []} totalUnique={ipHistory?.totalUnique ?? 0} />
  );
  const limitsCard = <LimitsCard client={client} />;

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
        statusLabel={statusLabel}
        status={status}
        hasFailedDeployment={hasFailedDeployment}
        onEdit={onEdit ? openEdit : undefined}
        onDisable={onDisable}
        onRedeploy={onRedeploy}
        redeploying={redeploying}
        onDelete={onDelete}
      />

      <div className="px-4 md:px-8 flex flex-col gap-6 pb-8 pt-6">
        <ClientDetailPulse client={client} />

        {/* Mobile: swipe tabs keep the scroll bounded on narrow viewports. */}
        <div className="md:hidden">
          <SwipeTabView
            tabs={[
              { id: "secret", label: t("detail.tabs.secret"), content: secretSection },
              { id: "deploy", label: t("detail.tabs.deploy"), content: deployLinks },
              { id: "ips", label: t("detail.tabs.ips"), content: ipHistoryCard },
              { id: "limits", label: t("detail.tabs.limits"), content: limitsCard },
            ]}
          />
        </div>

        {/* Desktop: stacked sections in reading order. */}
        <div className="hidden md:flex flex-col gap-5">
          {secretSection}
          {deployLinks}
          {ipHistoryCard}
          {limitsCard}
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
