// Phase-7 redesign: hero band + pulse row + separate Secret section
// + combined Deployments & Links + GeoIP-ready IP history +
// always-visible Limits card.
import { useState } from "react";

import { ClientFormSheet } from "@/features/clients/ClientFormSheet";
import {
  Badge,
  Breadcrumbs,
  Button,
  CopyButton,
  DataTable,
  HeroStrip,
  KvGrid,
  MonoValue,
  PageHeader,
  PulseRow,
  Sheet,
  SheetBody,
  SheetContent,
  SwipeTabView,
  deployVariant,
  formatBytes,
  formatExpiry,
  formatQuota,
  type ClientDeploymentData,
  type ClientDetailPageProps,
  type ClientFormData,
  type HeroMetaPill,
  type PulseTick,
  type StatusTone,
} from "@/ui";

// ─── Helpers ─────────────────────────────────────────────────────────

function isExpired(rfc: string): boolean {
  if (!rfc) return false;
  const t = Date.parse(rfc);
  return Number.isFinite(t) && t < Date.now();
}
function clientStatus(enabled: boolean, rfc: string): "active" | "disabled" | "expired" {
  if (isExpired(rfc)) return "expired";
  return enabled ? "active" : "disabled";
}
function expiresSuffix(rfc: string): string {
  if (!rfc) return "never";
  const t = Date.parse(rfc);
  if (!Number.isFinite(t)) return "—";
  const days = Math.floor((t - Date.now()) / (1000 * 60 * 60 * 24));
  if (days < 0) return `${Math.abs(days)}d ago`;
  if (days === 0) return "today";
  return `in ${days}d`;
}
function expiresTone(rfc: string): "default" | "warn" | "error" {
  if (!rfc) return "default";
  const t = Date.parse(rfc);
  if (!Number.isFinite(t)) return "default";
  const days = Math.floor((t - Date.now()) / (1000 * 60 * 60 * 24));
  if (days < 0) return "error";
  if (days < 7) return "warn";
  return "default";
}

// ─── Secret section ──────────────────────────────────────────────────

function SecretSection({
  secret,
  onRotate,
  rotating,
  pendingRedeploy,
}: {
  secret: string;
  onRotate?: () => void;
  rotating?: boolean;
  pendingRedeploy?: boolean;
}) {
  // Client secrets need a long-lived reveal/copy flow, not the one-shot
  // <SecretReveal> primitive used for TOTP bootstraps.
  const [revealed, setRevealed] = useState(false);
  // The Panvex API ships `secret` only on create + rotate responses
  // (omitempty on the regular GET). When it's absent we tell the
  // operator up front instead of showing a broken Reveal toggle —
  // tracked as backend follow-up #4.
  const hasSecret = !!secret;
  const masked = hasSecret ? "•".repeat(Math.min(32, Math.max(8, secret.length))) : "";
  return (
    <section className="rounded-xs bg-bg-card border border-divider p-4 flex flex-col gap-3">
      <div className="flex items-center justify-between gap-3 flex-wrap">
        <div className="flex items-baseline gap-2">
          <span className="text-sm font-semibold text-fg">Secret</span>
          <span className="text-[11px] font-mono text-fg-muted">
            rotating invalidates every outstanding connection link
          </span>
        </div>
        {onRotate && (
          <Button size="sm" variant="outline" disabled={rotating} onClick={onRotate}>
            {rotating ? "Rotating…" : "Rotate secret"}
          </Button>
        )}
      </div>
      {hasSecret ? (
        <div className="flex items-center gap-2 rounded-xs bg-bg border border-divider px-3 py-2 min-w-0">
          <code className="flex-1 min-w-0 text-sm font-mono text-fg break-all select-all">
            {revealed ? secret : masked}
          </code>
          <Button
            size="sm"
            variant="ghost"
            onClick={() => setRevealed((v) => !v)}
            className="shrink-0"
          >
            {revealed ? "Hide" : "Reveal"}
          </Button>
          <CopyButton text={secret} />
        </div>
      ) : (
        <div className="rounded-xs bg-bg border border-dashed border-divider px-3 py-2 text-[11px] font-mono text-fg-muted">
          Current secret isn't returned by the detail API — extract it from a
          fresh connection link below, or rotate to get a new one.
        </div>
      )}
      {pendingRedeploy && (
        <div className="text-[11px] font-mono text-status-warn">
          Secret rotated — wait for agents to re-apply before distributing new links.
        </div>
      )}
    </section>
  );
}

// ─── Deployments + Links (combined) ─────────────────────────────────

function LinksStrip({
  links,
}: {
  links: { classic: string[]; secure: string[]; tls: string[] };
}) {
  type LinkGroup = { key: "tls" | "secure" | "classic"; label: string; items: string[] };
  const groups: LinkGroup[] = (
    [
      { key: "tls", label: "TLS", items: links.tls },
      { key: "secure", label: "Secure", items: links.secure },
      { key: "classic", label: "Classic", items: links.classic },
    ] satisfies LinkGroup[]
  ).filter((g) => g.items.length > 0);
  if (groups.length === 0) {
    return (
      <div className="mt-2 text-[11px] font-mono text-fg-muted">No links generated yet.</div>
    );
  }
  return (
    <div className="mt-2 flex flex-col gap-1.5">
      {groups.map((g) => (
        <div key={g.key} className="flex items-center gap-2 min-w-0">
          <span className="text-[10px] font-mono uppercase tracking-wider text-fg-muted shrink-0 w-[56px]">
            {g.label}
          </span>
          <span className="font-mono text-xs text-fg truncate min-w-0 flex-1">
            {g.items[0]}
          </span>
          <CopyButton text={g.items[0] ?? ""} />
          {g.items.length > 1 && (
            <span className="text-[10px] font-mono text-fg-muted shrink-0">
              +{g.items.length - 1}
            </span>
          )}
        </div>
      ))}
    </div>
  );
}

function DeployLinksCard({
  deployments,
  secretPendingRedeploy,
  agentLabels,
}: {
  deployments: ClientDeploymentData[];
  secretPendingRedeploy?: boolean;
  agentLabels?: Record<string, string>;
}) {
  if (deployments.length === 0) {
    return (
      <div className="rounded-xs bg-bg-card border border-divider p-4 text-sm text-fg-muted text-center">
        No deployments yet.
      </div>
    );
  }
  return (
    <section className="rounded-xs bg-bg-card border border-divider overflow-hidden">
      <header className="px-4 py-3 border-b border-divider flex items-center justify-between gap-2">
        <div className="flex items-baseline gap-2">
          <span className="text-sm font-semibold text-fg">Deployments & links</span>
          <span className="text-[11px] font-mono text-fg-muted">
            {deployments.length} node{deployments.length === 1 ? "" : "s"}
          </span>
        </div>
        {secretPendingRedeploy && <Badge variant="warn">Secret rotated — awaiting redeploy</Badge>}
      </header>
      <div className="flex flex-col">
        {deployments.map((d) => {
          const tone = deployVariant(d.status);
          const label = agentLabels?.[d.agentId];
          return (
            <div key={d.agentId} className="px-4 py-3 border-b border-divider last:border-b-0">
              <div className="flex items-center gap-2 flex-wrap">
                <span className="font-mono text-sm text-fg truncate">
                  {label ?? d.agentId}
                </span>
                {label && (
                  <span className="font-mono text-[10px] text-fg-muted truncate">
                    {d.agentId.slice(0, 8)}…
                  </span>
                )}
                <Badge variant={tone}>{d.status}</Badge>
                {d.desiredOperation && d.desiredOperation !== "none" && (
                  <Badge variant="accent">{d.desiredOperation}</Badge>
                )}
                <span className="ml-auto text-[11px] font-mono text-fg-muted tabular-nums">
                  {d.lastAppliedAtUnix > 0
                    ? new Date(d.lastAppliedAtUnix * 1000).toLocaleString()
                    : "never applied"}
                </span>
              </div>
              {d.lastError && (
                <div className="mt-1 text-[11px] font-mono text-status-error break-words">
                  {d.lastError}
                </div>
              )}
              <LinksStrip links={d.links} />
            </div>
          );
        })}
      </div>
    </section>
  );
}

// ─── IP history (GeoIP-ready) ────────────────────────────────────────

interface IPRow {
  agentId: string;
  ip: string;
  firstSeen: string;
  lastSeen: string;
  countryCode?: string;
  countryName?: string;
  city?: string;
  asn?: string;
}

function IPHistoryCard({ ips, totalUnique }: { ips: IPRow[]; totalUnique: number }) {
  const columns = [
    {
      key: "ip",
      header: "IP",
      render: (row: IPRow) => <MonoValue>{row.ip}</MonoValue>,
      className: "w-[160px]",
    },
    {
      key: "country",
      header: "Country",
      render: (row: IPRow) =>
        row.countryName || row.countryCode ? (
          <span className="text-xs text-fg">
            {row.countryCode && (
              <span className="font-mono text-[10px] text-fg-muted mr-1">{row.countryCode}</span>
            )}
            {row.countryName ?? ""}
          </span>
        ) : (
          <span className="text-xs text-fg-faint">—</span>
        ),
      className: "hidden md:table-cell w-[160px]",
    },
    {
      key: "city",
      header: "City",
      render: (row: IPRow) =>
        row.city ? (
          <span className="text-xs text-fg">{row.city}</span>
        ) : (
          <span className="text-xs text-fg-faint">—</span>
        ),
      className: "hidden lg:table-cell w-[140px]",
    },
    {
      key: "asn",
      header: "ASN",
      render: (row: IPRow) =>
        row.asn ? (
          <MonoValue className="text-xs">{row.asn}</MonoValue>
        ) : (
          <span className="text-xs text-fg-faint">—</span>
        ),
      className: "hidden xl:table-cell w-[120px]",
    },
    {
      key: "firstSeen",
      header: "First seen",
      render: (row: IPRow) => (
        <span className="text-[11px] font-mono text-fg-muted tabular-nums">
          {new Date(row.firstSeen).toLocaleString()}
        </span>
      ),
      className: "hidden md:table-cell w-[170px]",
    },
    {
      key: "lastSeen",
      header: "Last seen",
      render: (row: IPRow) => (
        <span className="text-[11px] font-mono text-fg tabular-nums">
          {new Date(row.lastSeen).toLocaleString()}
        </span>
      ),
      className: "w-[170px]",
    },
  ];
  return (
    <section className="rounded-xs bg-bg-card border border-divider overflow-hidden">
      <header className="px-4 py-3 border-b border-divider flex items-center justify-between gap-2">
        <div className="flex items-baseline gap-2">
          <span className="text-sm font-semibold text-fg">IP history</span>
          <span className="text-[11px] font-mono text-fg-muted">{totalUnique} unique</span>
        </div>
        <span className="text-[10px] font-mono text-fg-muted truncate">
          GeoIP enrichment pending — see backend-followup #3
        </span>
      </header>
      {ips.length === 0 ? (
        <div className="px-4 py-8 text-sm text-fg-muted text-center">
          No IP activity recorded.
        </div>
      ) : (
        <DataTable
          columns={columns}
          data={ips}
          keyExtractor={(row) => `${row.agentId}:${row.ip}`}
          emptyMessage="No IPs"
        />
      )}
    </section>
  );
}

// ─── Limits & metadata ───────────────────────────────────────────────

function LimitsCard({ client }: { client: ClientDetailPageProps["client"] }) {
  return (
    <section className="rounded-xs bg-bg-card border border-divider p-4 flex flex-col gap-3">
      <span className="text-sm font-semibold text-fg">Limits & metadata</span>
      <KvGrid
        rows={[
          {
            label: "Ad tag",
            value: client.userAdTag ? (
              <MonoValue>{client.userAdTag}</MonoValue>
            ) : (
              <span className="text-xs text-fg-faint">—</span>
            ),
          },
          {
            label: "Max TCP conns",
            value: (
              <MonoValue>
                {client.maxTcpConns > 0 ? client.maxTcpConns : "Unlimited"}
              </MonoValue>
            ),
          },
          {
            label: "Max unique IPs",
            value: (
              <MonoValue>
                {client.maxUniqueIps > 0 ? client.maxUniqueIps : "Unlimited"}
              </MonoValue>
            ),
          },
          {
            label: "Quota",
            value: <MonoValue>{formatQuota(client.dataQuotaBytes)}</MonoValue>,
          },
          {
            label: "Fleet groups",
            value:
              client.fleetGroupIds.length === 0 ? (
                <span className="text-xs text-fg-faint">—</span>
              ) : (
                <div className="flex flex-wrap gap-1">
                  {client.fleetGroupIds.map((g) => (
                    <Badge key={g} variant="default">
                      {g}
                    </Badge>
                  ))}
                </div>
              ),
          },
        ]}
      />
    </section>
  );
}

// ─── Main page ───────────────────────────────────────────────────────

export function ClientDetailPage({
  client,
  onBack,
  onEdit,
  editLoading,
  editError,
  onRotateSecret,
  secretRotating,
  secretPendingRedeploy,
  onDisable,
  onDelete,
  ipHistory,
  agentLabels,
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
              <Button size="sm" onClick={() => setEditOpen(true)}>
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
              <Button size="sm" variant="outline" onClick={() => setEditOpen(true)}>
                Edit
              </Button>
            )}
            {onDisable && (
              <Button size="sm" variant="ghost" onClick={onDisable}>
                {client.enabled ? "Disable" : "Enable"}
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
              />
            </SheetBody>
          </SheetContent>
        </Sheet>
      )}

    </>
  );
}
