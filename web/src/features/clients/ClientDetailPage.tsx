// Phase-7 redesign: hero band + pulse row + separate Secret section
// + combined Deployments & Links + GeoIP-ready IP history +
// always-visible Limits card.
//
// R-Q-08: SecretSection, DeployLinksCard, IPHistoryCard, LimitsCard,
// and the expiry/status helpers all live in `./components/` so this
// file is left with composition and form-sheet plumbing only.
import { useState } from "react";

import { ClientFormSheet } from "@/features/clients/ClientFormSheet";
import { DeployLinksCard } from "./components/DeployLinksCard";
import { IPHistoryCard } from "./components/IPHistoryCard";
import { LimitsCard } from "./components/LimitsCard";
import { SecretSection } from "./components/SecretSection";
import {
  clientStatus,
  expiresSuffix,
  expiresTone,
} from "./components/clientDetailHelpers";
import {
  Breadcrumbs,
  Button,
  HeroStrip,
  PageHeader,
  PulseRow,
  Sheet,
  SheetBody,
  SheetContent,
  SwipeTabView,
  formatBytes,
  formatExpiry,
  formatQuota,
  type ClientDetailPageProps,
  type ClientFormData,
  type HeroMetaPill,
  type PulseTick,
  type StatusTone,
} from "@/ui";

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
}: ClientDetailPageProps) {
  // Expose "Redeploy" as a prominent action whenever at least one
  // deployment landed in `failed` state. Without this button operators
  // get stuck — they see an angry red status per node but have no
  // direct way to retry the rollout short of editing fields.
  const hasFailedDeployment =
    onRedeploy !== undefined &&
    client.deployments.some((d) => d.status === "failed");
  const [editOpen, setEditOpen] = useState(false);
  // Reseed the form each time the sheet opens — editing a client whose
  // assignments were just changed elsewhere (e.g. fleet-group rename)
  // should start from the latest server snapshot, not a stale draft.
  const openEdit = () => {
    setEditData({
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
    });
    setEditOpen(true);
  };
  const [editData, setEditData] = useState<ClientFormData>({
    name: client.name,
    userAdTag: client.userAdTag,
    userAdTagAuto: false,
    expirationRfc3339: client.expirationRfc3339,
    maxTcpConns: client.maxTcpConns,
    maxUniqueIps: client.maxUniqueIps,
    dataQuotaBytes: client.dataQuotaBytes,
    fleetGroupIds: client.fleetGroupIds,
    agentIds: client.agentIds,
  });

  const status = clientStatus(client.enabled, client.expirationRfc3339);
  const statusLabel =
    status === "expired" ? "EXPIRED" : status === "disabled" ? "DISABLED" : "ACTIVE";

  const trafficPct = client.dataQuotaBytes
    ? Math.min(100, (client.trafficUsedBytes / client.dataQuotaBytes) * 100)
    : undefined;
  const trafficTone: "default" | "ok" | "warn" | "error" =
    typeof trafficPct === "number"
      ? trafficPct >= 100
        ? "error"
        : trafficPct >= 80
          ? "warn"
          : "ok"
      : "default";
  const connsPct =
    client.maxTcpConns > 0 ? (client.activeTcpConns / client.maxTcpConns) * 100 : undefined;
  const connsTone: "default" | "warn" | "error" =
    typeof connsPct === "number"
      ? connsPct >= 100
        ? "error"
        : connsPct >= 80
          ? "warn"
          : "default"
      : "default";
  const ipsPct =
    client.maxUniqueIps > 0 ? (client.uniqueIpsUsed / client.maxUniqueIps) * 100 : undefined;
  const ipsTone: "default" | "warn" | "error" =
    typeof ipsPct === "number"
      ? ipsPct >= 100
        ? "error"
        : ipsPct >= 80
          ? "warn"
          : "default"
      : "default";

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
        <Breadcrumbs items={[{ label: "Clients", onClick: onBack }, { label: client.name }]} />
      </div>

      {/* Mobile — PageHeader carries name + status subtitle. */}
      <div className="md:hidden">
        <PageHeader
          title={client.name}
          subtitle={`${statusLabel.toLowerCase()} · ${expiresSuffix(client.expirationRfc3339)}`}
          trailing={
            onEdit ? (
              <Button size="sm" onClick={openEdit}>
                Edit
              </Button>
            ) : undefined
          }
        />
      </div>

      {/* Desktop hero — full-bleed band, matches the Server detail style. */}
      <HeroStrip
        className="hidden md:flex"
        name={client.name}
        status={{
          tone:
            status === "expired"
              ? "error"
              : client.enabled
                ? "ok"
                : "warn",
          label: statusLabel,
        }}
        pills={[
          ...client.fleetGroupIds.map<HeroMetaPill>((g) => ({
            label: "group",
            value: g,
            mono: true,
          })),
          {
            label: "expires",
            value: expiresSuffix(client.expirationRfc3339),
            mono: true,
            tone: expiresTone(client.expirationRfc3339) as StatusTone,
          },
        ]}
        actions={
          <>
            {onEdit && (
              <Button size="sm" variant="outline" onClick={openEdit}>
                Edit
              </Button>
            )}
            {onDisable && (
              <Button size="sm" variant="ghost" onClick={onDisable}>
                {client.enabled ? "Disable" : "Enable"}
              </Button>
            )}
            {hasFailedDeployment && onRedeploy && (
              <Button
                size="sm"
                variant="outline"
                onClick={onRedeploy}
                disabled={redeploying}
                className="text-status-warn border-status-warn/60 hover:text-status-warn"
                title="Re-run the client rollout to every target node"
              >
                {redeploying ? "Redeploying…" : "Redeploy"}
              </Button>
            )}
            {onDelete && (
              <Button
                size="sm"
                variant="ghost"
                onClick={onDelete}
                className="text-status-error hover:text-status-error"
              >
                Delete
              </Button>
            )}
          </>
        }
      />

      <div className="px-4 md:px-8 flex flex-col gap-6 pb-8 pt-6">
        <PulseRow
          ticks={[
            {
              label: "Connections",
              value: client.activeTcpConns.toLocaleString(),
              hint:
                client.maxTcpConns > 0
                  ? `of ${client.maxTcpConns.toLocaleString()} max`
                  : "no limit",
              tone: connsTone,
              barPct: connsPct,
            },
            {
              label: "Unique IPs",
              value: client.uniqueIpsUsed.toLocaleString(),
              hint:
                client.maxUniqueIps > 0
                  ? `of ${client.maxUniqueIps.toLocaleString()} max`
                  : "no limit",
              tone: ipsTone,
              barPct: ipsPct,
            },
            {
              label: "Traffic",
              value: formatBytes(client.trafficUsedBytes),
              hint:
                client.dataQuotaBytes > 0
                  ? `of ${formatQuota(client.dataQuotaBytes)}`
                  : "no quota",
              tone: trafficTone,
              barPct: trafficPct,
            },
            {
              label: "Expires",
              value: formatExpiry(client.expirationRfc3339),
              hint: expiresSuffix(client.expirationRfc3339),
              tone: expiresTone(client.expirationRfc3339),
            },
          ] satisfies PulseTick[]}
        />

        {/* Mobile: swipe tabs keep the scroll bounded on narrow viewports. */}
        <div className="md:hidden">
          <SwipeTabView
            tabs={[
              { id: "secret", label: "Secret", content: secretSection },
              { id: "deploy", label: "Deployments", content: deployLinks },
              { id: "ips", label: "IP history", content: ipHistoryCard },
              { id: "limits", label: "Limits", content: limitsCard },
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
        <Sheet
          open={editOpen}
          onOpenChange={(open) => {
            if (!open) setEditOpen(false);
          }}
        >
          <SheetContent
            side="bottom"
            title="Edit client"
            onOpenChange={(open) => {
              if (!open) setEditOpen(false);
            }}
          >
            <SheetBody>
              <ClientFormSheet
                mode="edit"
                data={editData}
                onChange={setEditData}
                onSubmit={async () => {
                  await onEdit(editData);
                  if (!editError) setEditOpen(false);
                }}
                onCancel={() => setEditOpen(false)}
                loading={editLoading}
                error={editError}
                fleetGroups={fleetGroups}
                agents={agents}
              />
            </SheetBody>
          </SheetContent>
        </Sheet>
      )}
    </>
  );
}
