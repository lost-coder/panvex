// P3-FE-01: recomposed locally from UI-kit primitives/components/compositions
// instead of importing the pre-built page from @lost-coder/panvex-ui/pages.
import { useState } from "react";
import {
  Badge,
  Breadcrumbs,
  Button,
  ClientFormSheet,
  ConfirmDialog,
  CopyButton,
  DataTable,
  FieldLabel,
  KvGrid,
  MonoValue,
  PageHeader,
  SectionHeader,
  Sheet,
  SheetBody,
  SheetContent,
  StatCard,
  SwipeTabView,
  deployVariant,
  formatBytes,
  formatExpiry,
  formatQuota,
  type ClientDeploymentData,
  type ClientDetailPageProps,
  type ClientFormData,
} from "@lost-coder/panvex-ui";

// ─── Connection links ────────────────────────────────────────────────────────

function ConnectionLinksContent({
  deployments,
  secretPendingRedeploy,
}: {
  deployments: ClientDeploymentData[];
  secretPendingRedeploy?: boolean;
}) {
  const activeLinks = deployments.filter(
    (d) =>
      d.status === "succeeded" &&
      (d.links.tls.length > 0 || d.links.classic.length > 0 || d.links.secure.length > 0),
  );

  return (
    <div className="flex flex-col">
      {secretPendingRedeploy && (
        <div className="px-4 py-2 text-xs font-medium text-status-warn bg-status-warn/10 border-b border-border">
          Secret changed — links will update after redeployment
        </div>
      )}
      {activeLinks.length === 0 ? (
        <div className="px-4 py-6 text-center text-fg-muted text-sm">
          Links available after deployment
        </div>
      ) : (
        <div className="flex flex-col divide-y divide-border">
          {activeLinks.map((d) => {
            const tlsFirst = d.links.tls[0];
            const classicFirst = d.links.classic[0];
            const secureFirst = d.links.secure[0];
            return (
              <div key={d.agentId} className="px-4 py-3 flex flex-col gap-1.5">
                <span className="text-caption uppercase tracking-wider">{d.agentId}</span>
                {tlsFirst && (
                  <div className="flex items-center gap-2">
                    <span className="text-[10px] text-fg-muted uppercase w-8 shrink-0">TLS</span>
                    <span className="text-xs font-mono text-fg truncate min-w-0">{tlsFirst}</span>
                    <CopyButton text={tlsFirst} />
                  </div>
                )}
                {classicFirst && (
                  <div className="flex items-center gap-2">
                    <span className="text-[10px] text-fg-muted uppercase w-8 shrink-0">t.me</span>
                    <span className="text-xs font-mono text-fg truncate min-w-0">
                      {classicFirst}
                    </span>
                    <CopyButton text={classicFirst} />
                  </div>
                )}
                {secureFirst && !classicFirst && (
                  <div className="flex items-center gap-2">
                    <span className="text-[10px] text-fg-muted uppercase w-8 shrink-0">Proxy</span>
                    <span className="text-xs font-mono text-fg truncate min-w-0">
                      {secureFirst}
                    </span>
                    <CopyButton text={secureFirst} />
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}

// ─── Overview content ─────────────────────────────────────────────────────────

function OverviewContent({
  client,
  onRotateSecret,
  secretRotating,
  secretPendingRedeploy,
}: {
  client: ClientDetailPageProps["client"];
  onRotateSecret?: () => void;
  secretRotating?: boolean;
  secretPendingRedeploy?: boolean;
}) {
  const settingsRows = [
    {
      label: "Max TCP Connections",
      value: <MonoValue>{client.maxTcpConns > 0 ? client.maxTcpConns : "Unlimited"}</MonoValue>,
    },
    {
      label: "Max Unique IPs",
      value: <MonoValue>{client.maxUniqueIps > 0 ? client.maxUniqueIps : "Unlimited"}</MonoValue>,
    },
    {
      label: "Data Quota",
      value: <MonoValue>{formatQuota(client.dataQuotaBytes)}</MonoValue>,
    },
    {
      label: "Expiration",
      value: <MonoValue>{formatExpiry(client.expirationRfc3339)}</MonoValue>,
    },
    {
      label: "Fleet Groups",
      value: (
        <MonoValue>
          {client.fleetGroupIds.length > 0 ? client.fleetGroupIds.join(", ") : "All"}
        </MonoValue>
      ),
    },
    {
      label: "Secret",
      value: (
        <span className="flex items-center gap-1">
          <MonoValue>{"••••••••"}</MonoValue>
          <CopyButton text={client.secret} />
          {onRotateSecret && (
            <Button size="sm" variant="ghost" onClick={onRotateSecret} disabled={secretRotating}>
              {secretRotating ? "Rotating..." : "Rotate"}
            </Button>
          )}
        </span>
      ),
    },
    {
      label: "User Ad Tag",
      value: <MonoValue className="text-fg-muted">{client.userAdTag || "—"}</MonoValue>,
    },
  ];

  return (
    <div className="flex flex-col gap-6">
      {/* Settings card */}
      <div className="rounded-xs bg-bg-card border border-border overflow-hidden">
        <div className="px-4 py-3 border-b border-border">
          <FieldLabel>Settings</FieldLabel>
        </div>
        <div className="p-4">
          <KvGrid rows={settingsRows} />
        </div>
      </div>

      {/* Connection links */}
      <div className="rounded-xs bg-bg-card border border-border overflow-hidden">
        <div className="px-4 py-3 border-b border-border">
          <FieldLabel>Connection Links</FieldLabel>
        </div>
        <ConnectionLinksContent
          deployments={client.deployments}
          secretPendingRedeploy={secretPendingRedeploy}
        />
      </div>
    </div>
  );
}

// ─── Deployments content ──────────────────────────────────────────────────────

function DeploymentsContent({ deployments }: { deployments: ClientDeploymentData[] }) {
  const deployColumns = [
    {
      key: "agentId",
      header: "Server",
      render: (row: ClientDeploymentData) => (
        <MonoValue className="truncate max-w-[120px]">{row.agentId}</MonoValue>
      ),
    },
    {
      key: "operation",
      header: "Operation",
      render: (row: ClientDeploymentData) => (
        <Badge variant="default">{row.desiredOperation}</Badge>
      ),
    },
    {
      key: "status",
      header: "Status",
      render: (row: ClientDeploymentData) => (
        <Badge variant={deployVariant(row.status)}>{row.status}</Badge>
      ),
    },
    {
      key: "lastApplied",
      header: "Last Applied",
      render: (row: ClientDeploymentData) => (
        <span className="font-mono text-xs text-fg-muted">
          {row.lastAppliedAtUnix > 0
            ? new Date(row.lastAppliedAtUnix * 1000).toLocaleString()
            : "—"}
        </span>
      ),
    },
    {
      key: "error",
      header: "Error",
      render: (row: ClientDeploymentData) =>
        row.lastError ? (
          <span
            className="text-xs text-status-error truncate max-w-[160px] block"
            title={row.lastError}
          >
            {row.lastError.slice(0, 60)}
            {row.lastError.length > 60 ? "…" : ""}
          </span>
        ) : (
          <span className="text-fg-muted text-xs">—</span>
        ),
    },
    {
      key: "link",
      header: "Links",
      render: (row: ClientDeploymentData) => {
        const tlsFirst = row.links.tls[0];
        const classicFirst = row.links.classic[0];
        if (!tlsFirst && !classicFirst) return <span className="text-fg-muted text-xs">—</span>;
        return (
          <span className="flex items-center gap-1">
            {tlsFirst && (
              <>
                <span className="text-[10px] text-fg-muted">TLS</span>
                <CopyButton text={tlsFirst} />
              </>
            )}
            {classicFirst && (
              <>
                <span className="text-[10px] text-fg-muted">t.me</span>
                <CopyButton text={classicFirst} />
              </>
            )}
          </span>
        );
      },
    },
  ];

  return (
    <div className="flex flex-col gap-4">
      <DataTable
        columns={deployColumns}
        data={deployments}
        keyExtractor={(row) => row.agentId}
        emptyMessage="No deployments"
      />
    </div>
  );
}

// ─── Main page ────────────────────────────────────────────────────────────────

export function ClientDetailPage({
  client,
  onBack,
  onEdit,
  editLoading,
  editError,
  onRotateSecret,
  secretRotating,
  secretPendingRedeploy,
  ipHistory,
}: ClientDetailPageProps) {
  const [editOpen, setEditOpen] = useState(false);
  const [editData, setEditData] = useState<ClientFormData>({
    name: client.name,
    userAdTag: client.userAdTag,
    expirationRfc3339: client.expirationRfc3339,
    maxTcpConns: client.maxTcpConns,
    maxUniqueIps: client.maxUniqueIps,
    dataQuotaBytes: client.dataQuotaBytes,
  });
  const [rotateConfirmOpen, setRotateConfirmOpen] = useState(false);

  function handleEditClick() {
    setEditData({
      name: client.name,
      userAdTag: client.userAdTag,
      expirationRfc3339: client.expirationRfc3339,
      maxTcpConns: client.maxTcpConns,
      maxUniqueIps: client.maxUniqueIps,
      dataQuotaBytes: client.dataQuotaBytes,
    });
    setEditOpen(true);
  }

  function handleRotateClick() {
    setRotateConfirmOpen(true);
  }

  const overviewContent = (
    <OverviewContent
      client={client}
      onRotateSecret={onRotateSecret ? handleRotateClick : undefined}
      secretRotating={secretRotating}
      secretPendingRedeploy={secretPendingRedeploy}
    />
  );
  const deploymentsContent = <DeploymentsContent deployments={client.deployments} />;

  const mobileTabs = [
    { id: "overview", label: "Overview", content: overviewContent },
    { id: "deployments", label: "Deployments", content: deploymentsContent },
    ...(ipHistory && ipHistory.ips.length > 0
      ? [
          {
            id: "ips",
            label: "IP History",
            content: (
              <div>
                <SectionHeader title="IP Address History" badge={ipHistory.totalUnique} />
                <div className="bg-bg-card border border-border rounded-xl overflow-hidden">
                  <DataTable
                    columns={[
                      {
                        key: "ip",
                        header: "IP Address",
                        render: (row: { ip: string }) => <MonoValue>{row.ip}</MonoValue>,
                      },
                      {
                        key: "lastSeen",
                        header: "Last Seen",
                        render: (row: { lastSeen: string }) => (
                          <span className="text-sm text-fg-muted">
                            {new Date(row.lastSeen).toLocaleString()}
                          </span>
                        ),
                      },
                    ]}
                    data={ipHistory.ips}
                    keyExtractor={(row) => `${row.agentId}-${row.ip}`}
                  />
                </div>
              </div>
            ),
          },
        ]
      : []),
  ];

  return (
    <>
      {/* Breadcrumbs */}
      <div className="px-4 md:px-8 pt-3">
        <Breadcrumbs items={[{ label: "Clients", onClick: onBack }, { label: client.name }]} />
      </div>

      {/* Page header */}
      <PageHeader
        title={client.name}
        trailing={
          <div className="flex items-center gap-2">
            <Badge variant={client.enabled ? "ok" : "error"}>
              {client.enabled ? "Active" : "Disabled"}
            </Badge>
            {onEdit && (
              <Button size="sm" variant="outline" onClick={handleEditClick}>
                Edit
              </Button>
            )}
          </div>
        }
      />

      <div className="px-4 md:px-8 flex flex-col gap-6 pb-8">
        {/* Stats strip */}
        <div className="grid grid-cols-2 md:grid-cols-4 gap-2">
          <StatCard label="Active Connections" value={String(client.activeTcpConns)} />
          <StatCard label="Unique IPs" value={String(client.uniqueIpsUsed)} />
          <StatCard label="Traffic Used" value={formatBytes(client.trafficUsedBytes)} />
          <StatCard label="Quota" value={formatQuota(client.dataQuotaBytes)} />
        </div>

        {/* Mobile: SwipeTabView */}
        <div className="md:hidden">
          <SwipeTabView tabs={mobileTabs} />
        </div>

        {/* Desktop: stacked sections */}
        <div className="hidden md:flex flex-col gap-6">
          <div>
            <SectionHeader title="Settings" />
            <div className="rounded-xs bg-bg-card border border-border p-4">
              <KvGrid
                rows={[
                  {
                    label: "Max TCP Connections",
                    value: (
                      <MonoValue>
                        {client.maxTcpConns > 0 ? client.maxTcpConns : "Unlimited"}
                      </MonoValue>
                    ),
                  },
                  {
                    label: "Max Unique IPs",
                    value: (
                      <MonoValue>
                        {client.maxUniqueIps > 0 ? client.maxUniqueIps : "Unlimited"}
                      </MonoValue>
                    ),
                  },
                  {
                    label: "Data Quota",
                    value: <MonoValue>{formatQuota(client.dataQuotaBytes)}</MonoValue>,
                  },
                  {
                    label: "Expiration",
                    value: <MonoValue>{formatExpiry(client.expirationRfc3339)}</MonoValue>,
                  },
                  {
                    label: "Fleet Groups",
                    value: (
                      <MonoValue>
                        {client.fleetGroupIds.length > 0 ? client.fleetGroupIds.join(", ") : "All"}
                      </MonoValue>
                    ),
                  },
                  {
                    label: "Secret",
                    value: (
                      <span className="flex items-center gap-1">
                        <MonoValue>{"••••••••"}</MonoValue>
                        <CopyButton text={client.secret} />
                        {onRotateSecret && (
                          <Button
                            size="sm"
                            variant="ghost"
                            onClick={handleRotateClick}
                            disabled={secretRotating}
                          >
                            {secretRotating ? "Rotating..." : "Rotate"}
                          </Button>
                        )}
                      </span>
                    ),
                  },
                  {
                    label: "User Ad Tag",
                    value: (
                      <MonoValue className="text-fg-muted">{client.userAdTag || "—"}</MonoValue>
                    ),
                  },
                ]}
              />
            </div>
          </div>

          <div>
            <SectionHeader title="Connection Links" />
            <div className="rounded-xs bg-bg-card border border-border overflow-hidden">
              <ConnectionLinksContent
                deployments={client.deployments}
                secretPendingRedeploy={secretPendingRedeploy}
              />
            </div>
          </div>

          <div>
            <SectionHeader title="Deployments" badge={client.deployments.length} />
            <DeploymentsContent deployments={client.deployments} />
          </div>

          {/* IP History */}
          {ipHistory && ipHistory.ips.length > 0 && (
            <div>
              <SectionHeader title="IP Address History" badge={ipHistory.totalUnique} />
              <div className="bg-bg-card border border-border rounded-xl overflow-hidden">
                <DataTable
                  columns={[
                    {
                      key: "ip",
                      header: "IP Address",
                      render: (row) => <MonoValue>{row.ip}</MonoValue>,
                    },
                    {
                      key: "firstSeen",
                      header: "First Seen",
                      render: (row) => (
                        <span className="text-sm text-fg-muted">
                          {new Date(row.firstSeen).toLocaleString()}
                        </span>
                      ),
                    },
                    {
                      key: "lastSeen",
                      header: "Last Seen",
                      render: (row) => (
                        <span className="text-sm text-fg-muted">
                          {new Date(row.lastSeen).toLocaleString()}
                        </span>
                      ),
                    },
                    {
                      key: "agentId",
                      header: "Server",
                      render: (row) => <span className="text-sm text-fg-muted">{row.agentId}</span>,
                    },
                  ]}
                  data={ipHistory.ips}
                  keyExtractor={(row) => `${row.agentId}-${row.ip}`}
                />
              </div>
            </div>
          )}
        </div>
      </div>

      {/* Edit Sheet */}
      {onEdit && (
        <Sheet
          open={editOpen}
          onOpenChange={(open) => {
            if (!open) setEditOpen(false);
          }}
        >
          <SheetContent side="bottom">
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
              />
            </SheetBody>
          </SheetContent>
        </Sheet>
      )}

      {/* Rotate Secret Confirm */}
      <ConfirmDialog
        open={rotateConfirmOpen}
        title="Rotate Secret"
        description="This will generate a new secret. All connection links will be regenerated after redeployment. Current links will stop working."
        confirmLabel="Rotate Secret"
        variant="danger"
        onConfirm={() => {
          setRotateConfirmOpen(false);
          onRotateSecret?.();
        }}
        onCancel={() => setRotateConfirmOpen(false)}
      />
    </>
  );
}
