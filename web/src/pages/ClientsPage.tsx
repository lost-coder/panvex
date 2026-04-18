// P3-FE-01: recomposed locally from UI-kit primitives/components/compositions
// instead of importing the pre-built page from @lost-coder/panvex-ui/pages.
import { useState } from "react";
import {
  Badge,
  Button,
  ClientFormSheet,
  DataTable,
  DiscoveredClientsBanner,
  FieldLabel,
  MonoValue,
  PageHeader,
  Sheet,
  SheetBody,
  SheetContent,
  StatusDot,
  TableView,
  deployVariant,
  formatBytes,
  formatExpiry,
  formatQuota,
  type ClientFormData,
  type ClientListItem,
  type ClientsPageProps,
  type ViewMode,
} from "@lost-coder/panvex-ui";

// ─── Desktop DataTable columns ────────────────────────────────────────────────

const columns = [
  {
    key: "name",
    header: "Name",
    render: (c: ClientListItem) => <span className="font-medium text-fg">{c.name}</span>,
  },
  {
    key: "status",
    header: "Status",
    render: (c: ClientListItem) => (
      <Badge variant={c.enabled ? "ok" : "error"}>{c.enabled ? "Active" : "Disabled"}</Badge>
    ),
  },
  {
    key: "conns",
    header: "Active Conns",
    render: (c: ClientListItem) => (
      <MonoValue className={c.activeTcpConns > 0 ? "text-fg" : "text-fg-muted"}>
        {c.activeTcpConns}
      </MonoValue>
    ),
  },
  {
    key: "traffic",
    header: "Traffic Used",
    render: (c: ClientListItem) => <MonoValue>{formatBytes(c.trafficUsedBytes)}</MonoValue>,
  },
  {
    key: "ips",
    header: "Unique IPs",
    render: (c: ClientListItem) => <MonoValue>{c.uniqueIpsUsed}</MonoValue>,
  },
  {
    key: "quota",
    header: "Quota",
    render: (c: ClientListItem) => (
      <MonoValue className="text-fg-muted">{formatQuota(c.dataQuotaBytes)}</MonoValue>
    ),
  },
  {
    key: "expiry",
    header: "Expiration",
    render: (c: ClientListItem) => (
      <MonoValue className="text-fg-muted">{formatExpiry(c.expirationRfc3339)}</MonoValue>
    ),
  },
  {
    key: "deploy",
    header: "Deploy",
    render: (c: ClientListItem) => (
      <Badge variant={deployVariant(c.lastDeployStatus)}>{c.lastDeployStatus}</Badge>
    ),
  },
  {
    key: "nodes",
    header: "Nodes",
    render: (c: ClientListItem) => (
      <MonoValue className="text-fg-muted">{c.assignedNodesCount}</MonoValue>
    ),
  },
];

// ─── Client card (cards view) ─────────────────────────────────────────────────

function ClientCard({ client, onClick }: { client: ClientListItem; onClick?: () => void }) {
  return (
    <div
      onClick={onClick}
      className="rounded-xs bg-bg-card border border-border p-4 flex flex-col gap-3 cursor-pointer hover:bg-bg-card-hover transition-colors"
    >
      {/* Header */}
      <div className="flex items-start justify-between gap-2">
        <span className="font-semibold text-fg truncate">{client.name}</span>
        <Badge variant={client.enabled ? "ok" : "error"}>
          {client.enabled ? "Active" : "Disabled"}
        </Badge>
      </div>

      {/* Stats grid */}
      <div className="grid grid-cols-2 gap-x-4 gap-y-1.5 text-sm">
        <FieldLabel>Active Conns</FieldLabel>
        <MonoValue className={client.activeTcpConns > 0 ? "text-fg" : "text-fg-muted"}>
          {client.activeTcpConns}
        </MonoValue>

        <FieldLabel>Traffic Used</FieldLabel>
        <MonoValue>{formatBytes(client.trafficUsedBytes)}</MonoValue>

        <FieldLabel>Unique IPs</FieldLabel>
        <MonoValue>{client.uniqueIpsUsed}</MonoValue>

        <FieldLabel>Quota</FieldLabel>
        <MonoValue className="text-fg-muted">{formatQuota(client.dataQuotaBytes)}</MonoValue>

        <FieldLabel>Expiration</FieldLabel>
        <MonoValue className="text-fg-muted">{formatExpiry(client.expirationRfc3339)}</MonoValue>

        <FieldLabel>Nodes</FieldLabel>
        <MonoValue className="text-fg-muted">{client.assignedNodesCount}</MonoValue>
      </div>

      {/* Footer */}
      <div className="flex items-center justify-between pt-1 border-t border-border">
        <FieldLabel>Deploy</FieldLabel>
        <Badge variant={deployVariant(client.lastDeployStatus)}>{client.lastDeployStatus}</Badge>
      </div>
    </div>
  );
}

// ─── Mobile list row ──────────────────────────────────────────────────────────

function ClientListRow({ client, onClick }: { client: ClientListItem; onClick?: () => void }) {
  return (
    <div
      onClick={onClick}
      className="flex items-center justify-between gap-3 px-4 py-3 border-b border-border hover:bg-bg-hover transition-colors cursor-pointer"
    >
      <div className="flex items-center gap-2 min-w-0">
        <StatusDot status={client.enabled ? "ok" : "error"} />
        <span className="font-medium text-fg truncate">{client.name}</span>
      </div>
      <div className="flex items-center gap-2 shrink-0">
        {client.activeTcpConns > 0 && (
          <MonoValue className="text-xs">{client.activeTcpConns} conns</MonoValue>
        )}
        <Badge variant={client.enabled ? "ok" : "error"}>
          {client.enabled ? "Active" : "Disabled"}
        </Badge>
      </div>
    </div>
  );
}

// ─── Main page ────────────────────────────────────────────────────────────────

const emptyFormData: ClientFormData = {
  name: "",
  userAdTag: "",
  expirationRfc3339: "",
  maxTcpConns: 0,
  maxUniqueIps: 0,
  dataQuotaBytes: 0,
};

export function ClientsPage({
  clients,
  viewMode,
  autoThreshold = 6,
  onViewModeChange,
  onClientClick,
  onCreate,
  createLoading,
  createError,
  pendingDiscoveredCount,
  onDiscoveredClick,
}: ClientsPageProps) {
  const [search, setSearch] = useState("");
  const [statusFilter, setStatusFilter] = useState("all");
  const [deployFilter, setDeployFilter] = useState("all");
  const [currentPage, setCurrentPage] = useState(1);
  const pageSize = 20;
  const [createOpen, setCreateOpen] = useState(false);
  const [createData, setCreateData] = useState<ClientFormData>({ ...emptyFormData });

  const effectiveMode: ViewMode = viewMode ?? (clients.length <= autoThreshold ? "cards" : "list");

  // Filter
  const filtered = clients.filter((c) => {
    const matchSearch = !search || c.name.toLowerCase().includes(search.toLowerCase());
    const matchStatus =
      statusFilter === "all" ||
      (statusFilter === "active" && c.enabled) ||
      (statusFilter === "disabled" && !c.enabled);
    const matchDeploy = deployFilter === "all" || c.lastDeployStatus === deployFilter;
    return matchSearch && matchStatus && matchDeploy;
  });

  // Paginate
  const totalPages = Math.max(1, Math.ceil(filtered.length / pageSize));
  const safePage = Math.min(currentPage, totalPages);
  const paginated =
    effectiveMode === "list"
      ? filtered.slice((safePage - 1) * pageSize, safePage * pageSize)
      : filtered;

  return (
    <>
      <PageHeader
        title="Clients"
        subtitle={`${clients.length} clients`}
        trailing={
          onCreate ? (
            <Button
              size="sm"
              onClick={() => {
                setCreateData({ ...emptyFormData });
                setCreateOpen(true);
              }}
            >
              Add Client
            </Button>
          ) : undefined
        }
      />
      <div className="px-4 md:px-8 pb-8">
        {!!pendingDiscoveredCount && (
          <div className="mb-5">
            <DiscoveredClientsBanner count={pendingDiscoveredCount} onClick={onDiscoveredClick} />
          </div>
        )}
        <TableView
          search={{
            value: search,
            onChange: (v) => {
              setSearch(v);
              setCurrentPage(1);
            },
            placeholder: "Search by name...",
          }}
          filters={[
            {
              key: "status",
              value: statusFilter,
              onChange: (v) => {
                setStatusFilter(v);
                setCurrentPage(1);
              },
              options: [
                { value: "all", label: "All Statuses" },
                { value: "active", label: "Active" },
                { value: "disabled", label: "Disabled" },
              ],
              placeholder: "Status",
            },
            {
              key: "deploy",
              value: deployFilter,
              onChange: (v) => {
                setDeployFilter(v);
                setCurrentPage(1);
              },
              options: [
                { value: "all", label: "All Deploy" },
                { value: "ok", label: "OK" },
                { value: "pending", label: "Pending" },
                { value: "error", label: "Error" },
              ],
              placeholder: "Deploy",
            },
          ]}
          viewMode={
            onViewModeChange ? { current: effectiveMode, onChange: onViewModeChange } : undefined
          }
          pagination={{
            page: safePage,
            totalPages,
            totalItems: filtered.length,
            pageSize,
            onChange: setCurrentPage,
          }}
        >
          {effectiveMode === "cards" ? (
            // Cards view
            <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
              {paginated.map((c) => (
                <ClientCard key={c.id} client={c} onClick={() => onClientClick?.(c.id)} />
              ))}
            </div>
          ) : (
            // List view
            <div className="bg-bg-card border border-border rounded-xl shadow-sm overflow-hidden">
              {/* Mobile: compact rows */}
              <div className="md:hidden flex flex-col">
                {paginated.map((c) => (
                  <ClientListRow key={c.id} client={c} onClick={() => onClientClick?.(c.id)} />
                ))}
              </div>
              {/* Desktop: DataTable */}
              <div className="hidden md:block">
                <DataTable
                  columns={columns}
                  data={paginated}
                  keyExtractor={(c) => c.id}
                  onRowClick={(c) => onClientClick?.(c.id)}
                />
              </div>
            </div>
          )}
        </TableView>
      </div>

      {onCreate && (
        <Sheet
          open={createOpen}
          onOpenChange={(open) => {
            if (!open) setCreateOpen(false);
          }}
        >
          <SheetContent side="bottom">
            <SheetBody>
              <ClientFormSheet
                mode="create"
                data={createData}
                onChange={setCreateData}
                onSubmit={async () => {
                  await onCreate(createData);
                  if (!createError) setCreateOpen(false);
                }}
                onCancel={() => setCreateOpen(false)}
                loading={createLoading}
                error={createError}
              />
            </SheetBody>
          </SheetContent>
        </Sheet>
      )}
    </>
  );
}
