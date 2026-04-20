import { useState } from "react";
import { AlertTriangle } from "lucide-react";
import { PageHeader } from "@/ui/layout/PageHeader";
import { Badge } from "@/ui/primitives/Badge";
import { MonoValue } from "@/ui/primitives/MonoValue";
import { FieldLabel } from "@/ui/primitives/FieldLabel";
import { DataTable } from "@/ui/components/DataTable";
import { EmptyState } from "@/ui/components/EmptyState";
import { Button } from "@/ui/base/button";
import { ConfirmDialog } from "@/ui/base/confirm-dialog";
import { formatBytes, formatQuota, formatAge } from "@/shared/lib/pages-shared";
import type { DiscoveredClientsPageProps, DiscoveredClientItem } from "@/shared/api/types-pages/pages";

// ─── Helpers ─────────────────────────────────────────────────────────────────

function statusVariant(status: string): "warn" | "ok" | "default" {
  if (status === "pending_review") return "warn";
  if (status === "adopted") return "ok";
  return "default";
}

function statusLabel(status: string): string {
  if (status === "pending_review") return "Pending";
  if (status === "adopted") return "Adopted";
  if (status === "ignored") return "Ignored";
  return status;
}

function conflictTooltip(dc: DiscoveredClientItem): string | null {
  if (!dc.conflicts?.length) return null;
  return dc.conflicts
    .map((c) =>
      c.type === "same_secret_different_names"
        ? "Same secret used by different client names"
        : "Same name has different secrets on other servers",
    )
    .join("; ");
}

// ─── Desktop DataTable columns ──────────────────────────────────────────────

const baseColumns = [
  {
    key: "clientName",
    header: "Name",
    render: (dc: DiscoveredClientItem) => (
      <span className="flex items-center gap-1.5 font-medium text-fg">
        {dc.clientName}
        {dc.conflicts?.length ? (
          <span title={conflictTooltip(dc) ?? ""}>
            <AlertTriangle className="w-3.5 h-3.5 text-amber-500" />
          </span>
        ) : null}
      </span>
    ),
  },
  {
    key: "server",
    header: "Server",
    render: (dc: DiscoveredClientItem) => (
      <span className="text-fg-muted">{dc.nodeName || dc.agentId.slice(0, 8)}</span>
    ),
  },
  {
    key: "status",
    header: "Status",
    render: (dc: DiscoveredClientItem) => (
      <Badge variant={statusVariant(dc.status)}>{statusLabel(dc.status)}</Badge>
    ),
  },
  {
    key: "conns",
    header: "Conns",
    render: (dc: DiscoveredClientItem) => (
      <MonoValue className={dc.currentConnections > 0 ? "text-fg" : "text-fg-muted"}>
        {dc.currentConnections}
      </MonoValue>
    ),
  },
  {
    key: "traffic",
    header: "Traffic",
    render: (dc: DiscoveredClientItem) => <MonoValue>{formatBytes(dc.totalOctets)}</MonoValue>,
  },
  {
    key: "ips",
    header: "Unique IPs",
    render: (dc: DiscoveredClientItem) => <MonoValue>{dc.activeUniqueIps}</MonoValue>,
  },
  {
    key: "quota",
    header: "Quota",
    render: (dc: DiscoveredClientItem) => (
      <MonoValue className="text-fg-muted">{formatQuota(dc.dataQuotaBytes)}</MonoValue>
    ),
  },
  {
    key: "discovered",
    header: "Discovered",
    render: (dc: DiscoveredClientItem) => (
      <MonoValue className="text-fg-muted">{formatAge(dc.discoveredAtUnix)}</MonoValue>
    ),
  },
];

// ─── Mobile row ─────────────────────────────────────────────────────────────

function DiscoveredClientRow({
  client,
  onAdopt,
  onIgnore,
  busy,
}: {
  client: DiscoveredClientItem;
  onAdopt?: () => void;
  onIgnore?: () => void;
  busy?: boolean;
}) {
  return (
    <div className="flex items-center justify-between gap-3 px-4 py-3 border-b border-border">
      <div className="flex flex-col gap-1 min-w-0">
        <span className="font-medium text-fg truncate">{client.clientName}</span>
        <div className="flex gap-2 text-xs text-fg-muted">
          <span>{client.nodeName || client.agentId.slice(0, 8)}</span>
          <span>{client.currentConnections} conns</span>
          <span>{formatBytes(client.totalOctets)}</span>
        </div>
      </div>
      <div className="flex items-center gap-2 shrink-0">
        <Badge variant={statusVariant(client.status)}>{statusLabel(client.status)}</Badge>
        {client.status === "pending_review" && onAdopt && onIgnore && (
          <>
            <Button size="sm" onClick={onAdopt} disabled={busy}>
              Adopt
            </Button>
            <Button size="sm" variant="outline" onClick={onIgnore} disabled={busy}>
              Ignore
            </Button>
          </>
        )}
      </div>
    </div>
  );
}

// ─── Client card (mobile cards view) ────────────────────────────────────────

function DiscoveredClientCard({
  client,
  selected,
  onToggle,
  onAdopt,
  onIgnore,
  busy,
}: {
  client: DiscoveredClientItem;
  selected?: boolean;
  onToggle?: () => void;
  onAdopt?: () => void;
  onIgnore?: () => void;
  busy?: boolean;
}) {
  return (
    <div
      className={`rounded-xs bg-bg-card border p-4 flex flex-col gap-3 ${selected ? "border-accent ring-1 ring-accent" : "border-border"}`}
    >
      {/* Header */}
      <div className="flex items-start justify-between gap-2">
        <div className="flex items-center gap-2 min-w-0">
          {onToggle && (
            <input
              type="checkbox"
              checked={selected}
              onChange={onToggle}
              className="w-4 h-4 rounded border-border accent-accent shrink-0"
            />
          )}
          <div className="flex items-center gap-1.5 min-w-0">
            <span className="font-semibold text-fg truncate">{client.clientName}</span>
            {client.conflicts?.length ? (
              <span title={conflictTooltip(client) ?? ""}>
                <AlertTriangle className="w-3.5 h-3.5 text-amber-500 shrink-0" />
              </span>
            ) : null}
          </div>
        </div>
        <Badge variant={statusVariant(client.status)}>{statusLabel(client.status)}</Badge>
      </div>

      {/* Stats grid */}
      <div className="grid grid-cols-2 gap-x-4 gap-y-1.5 text-sm">
        <FieldLabel>Server</FieldLabel>
        <span className="text-fg-muted text-sm">
          {client.nodeName || client.agentId.slice(0, 8)}
        </span>

        <FieldLabel>Active Conns</FieldLabel>
        <MonoValue className={client.currentConnections > 0 ? "text-fg" : "text-fg-muted"}>
          {client.currentConnections}
        </MonoValue>

        <FieldLabel>Traffic</FieldLabel>
        <MonoValue>{formatBytes(client.totalOctets)}</MonoValue>

        <FieldLabel>Unique IPs</FieldLabel>
        <MonoValue>{client.activeUniqueIps}</MonoValue>

        <FieldLabel>Quota</FieldLabel>
        <MonoValue className="text-fg-muted">{formatQuota(client.dataQuotaBytes)}</MonoValue>

        <FieldLabel>Discovered</FieldLabel>
        <MonoValue className="text-fg-muted">{formatAge(client.discoveredAtUnix)}</MonoValue>
      </div>

      {/* Actions */}
      {client.status === "pending_review" && onAdopt && onIgnore && (
        <div className="flex gap-2 pt-1 border-t border-border">
          <Button size="sm" className="flex-1" onClick={onAdopt} disabled={busy}>
            Adopt
          </Button>
          <Button size="sm" variant="outline" className="flex-1" onClick={onIgnore} disabled={busy}>
            Ignore
          </Button>
        </div>
      )}
    </div>
  );
}

// ─── Main page ──────────────────────────────────────────────────────────────

export function DiscoveredClientsPage({
  clients,
  onAdopt,
  onIgnore,
  onAdoptMany,
  onIgnoreMany,
  onBack,
  busy,
}: DiscoveredClientsPageProps) {
  const [confirmAction, setConfirmAction] = useState<{
    type: "adopt" | "ignore";
    id: string;
    name: string;
  } | null>(null);

  const [bulkConfirmAction, setBulkConfirmAction] = useState<"adopt" | "ignore" | null>(null);
  const [selected, setSelected] = useState<Set<string>>(new Set());

  const pending = clients.filter((dc) => dc.status === "pending_review");
  const rest = clients.filter((dc) => dc.status !== "pending_review");

  const toggleSelection = (id: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const toggleAll = () => {
    if (selected.size === pending.length) {
      setSelected(new Set());
    } else {
      setSelected(new Set(pending.map((dc) => dc.id)));
    }
  };

  const handleConfirm = () => {
    if (!confirmAction) return;
    if (confirmAction.type === "adopt") {
      onAdopt?.(confirmAction.id);
    } else {
      onIgnore?.(confirmAction.id);
    }
    setConfirmAction(null);
  };

  const handleBulkConfirm = () => {
    if (!bulkConfirmAction) return;
    const ids = [...selected];
    if (bulkConfirmAction === "adopt") {
      onAdoptMany?.(ids);
    } else {
      onIgnoreMany?.(ids);
    }
    setSelected(new Set());
    setBulkConfirmAction(null);
  };

  const selectColumns = [
    {
      key: "select",
      header: "",
      className: "w-10",
      render: (dc: DiscoveredClientItem) => (
        <input
          type="checkbox"
          checked={selected.has(dc.id)}
          onChange={(e: React.ChangeEvent) => {
            e.stopPropagation();
            toggleSelection(dc.id);
          }}
          onClick={(e: React.MouseEvent) => e.stopPropagation()}
          className="w-4 h-4 rounded border-border accent-accent"
        />
      ),
    },
    ...baseColumns,
    {
      key: "actions",
      header: "",
      render: (dc: DiscoveredClientItem) => (
        <div className="flex gap-2 justify-end">
          <Button
            size="sm"
            onClick={(e: React.MouseEvent) => {
              e.stopPropagation();
              setConfirmAction({ type: "adopt", id: dc.id, name: dc.clientName });
            }}
            disabled={busy}
          >
            Adopt
          </Button>
          <Button
            size="sm"
            variant="outline"
            onClick={(e: React.MouseEvent) => {
              e.stopPropagation();
              setConfirmAction({ type: "ignore", id: dc.id, name: dc.clientName });
            }}
            disabled={busy}
          >
            Ignore
          </Button>
        </div>
      ),
    },
  ];

  return (
    <>
      <PageHeader
        title="Discovered Clients"
        subtitle={`${pending.length} pending review`}
        trailing={
          onBack ? (
            <Button size="sm" variant="outline" onClick={onBack}>
              Back to Clients
            </Button>
          ) : undefined
        }
      />
      <div className="px-4 md:px-8 pb-8 flex flex-col gap-6">
        {pending.length === 0 && rest.length === 0 ? (
          <EmptyState
            title="No discovered clients"
            description="When agents report users that are not managed by the panel, they will appear here for review."
          />
        ) : (
          <>
            {pending.length > 0 && (
              <section>
                <div className="flex items-center justify-between mb-3">
                  <h3 className="text-sm font-medium text-fg-muted">
                    Pending Review ({pending.length})
                  </h3>
                  {(onAdoptMany || onIgnoreMany) && (
                    <div className="flex items-center gap-2">
                      <Button size="sm" variant="outline" onClick={toggleAll} disabled={busy}>
                        {selected.size === pending.length ? "Deselect all" : "Select all"}
                      </Button>
                      {selected.size > 0 && (
                        <span className="text-xs text-fg-muted">{selected.size} selected</span>
                      )}
                      {selected.size > 0 && onAdoptMany && (
                        <Button
                          size="sm"
                          onClick={() => setBulkConfirmAction("adopt")}
                          disabled={busy}
                        >
                          Adopt {selected.size}
                        </Button>
                      )}
                      {selected.size > 0 && onIgnoreMany && (
                        <Button
                          size="sm"
                          variant="outline"
                          onClick={() => setBulkConfirmAction("ignore")}
                          disabled={busy}
                        >
                          Ignore {selected.size}
                        </Button>
                      )}
                    </div>
                  )}
                </div>
                {/* Mobile: cards */}
                <div className="md:hidden grid grid-cols-1 gap-3">
                  {pending.map((dc) => (
                    <DiscoveredClientCard
                      key={dc.id}
                      client={dc}
                      selected={selected.has(dc.id)}
                      onToggle={() => toggleSelection(dc.id)}
                      onAdopt={() =>
                        setConfirmAction({ type: "adopt", id: dc.id, name: dc.clientName })
                      }
                      onIgnore={() =>
                        setConfirmAction({ type: "ignore", id: dc.id, name: dc.clientName })
                      }
                      busy={busy}
                    />
                  ))}
                </div>
                {/* Desktop: DataTable */}
                <div className="hidden md:block bg-bg-card border border-border rounded-xl shadow-sm overflow-hidden">
                  <DataTable
                    columns={selectColumns}
                    data={pending}
                    keyExtractor={(dc: DiscoveredClientItem) => dc.id}
                    onRowClick={(dc: DiscoveredClientItem) => toggleSelection(dc.id)}
                  />
                </div>
              </section>
            )}

            {rest.length > 0 && (
              <section>
                <h3 className="text-sm font-medium text-fg-muted mb-3">
                  Previously Reviewed ({rest.length})
                </h3>
                {/* Mobile */}
                <div className="md:hidden flex flex-col bg-bg-card border border-border rounded-xl shadow-sm overflow-hidden">
                  {rest.map((dc) => (
                    <DiscoveredClientRow key={dc.id} client={dc} />
                  ))}
                </div>
                {/* Desktop */}
                <div className="hidden md:block bg-bg-card border border-border rounded-xl shadow-sm overflow-hidden">
                  <DataTable
                    columns={baseColumns}
                    data={rest}
                    keyExtractor={(dc: DiscoveredClientItem) => dc.id}
                  />
                </div>
              </section>
            )}
          </>
        )}
      </div>

      {confirmAction && (
        <ConfirmDialog
          title={confirmAction.type === "adopt" ? "Adopt Client" : "Ignore Client"}
          description={
            confirmAction.type === "adopt"
              ? `Import "${confirmAction.name}" as a managed client? The existing secret and connection link will be preserved.`
              : `Ignore "${confirmAction.name}"? It will not appear in pending review unless you reset it.`
          }
          confirmLabel={confirmAction.type === "adopt" ? "Adopt" : "Ignore"}
          variant={confirmAction.type === "ignore" ? "danger" : "default"}
          open={true}
          onConfirm={handleConfirm}
          onCancel={() => setConfirmAction(null)}
        />
      )}

      {bulkConfirmAction && (
        <ConfirmDialog
          title={
            bulkConfirmAction === "adopt"
              ? `Adopt ${selected.size} Clients`
              : `Ignore ${selected.size} Clients`
          }
          description={
            bulkConfirmAction === "adopt"
              ? `Import ${selected.size} selected clients? Their existing secrets and connection links will be preserved.`
              : `Ignore ${selected.size} selected clients? They will not appear in pending review unless reset.`
          }
          confirmLabel={
            bulkConfirmAction === "adopt" ? `Adopt ${selected.size}` : `Ignore ${selected.size}`
          }
          variant={bulkConfirmAction === "ignore" ? "danger" : "default"}
          open={true}
          onConfirm={handleBulkConfirm}
          onCancel={() => setBulkConfirmAction(null)}
        />
      )}
    </>
  );
}
