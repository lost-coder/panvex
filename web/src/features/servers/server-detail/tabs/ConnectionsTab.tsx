import { FieldLabel, MonoValue } from "@/ui/primitives";
import { SectionHeader } from "@/ui/layout/SectionHeader";
import { ProgressBar } from "@/ui/primitives/ProgressBar";
import { Badge } from "@/ui/primitives/Badge";
import { DataTable } from "@/ui/components/DataTable";
import { formatBytes } from "@/ui/lib/format";
import type { ServerDetailPageProps } from "@/shared/api/types-pages/pages";

export function ConnectionsTab({ server }: { server: ServerDetailPageProps["server"] }) {
  const { connections, summary } = server;
  const total = connections.current;
  const mePct = total > 0 ? Math.round((connections.currentMe / total) * 100) : 0;
  const directPct = total > 0 ? Math.round((connections.currentDirect / total) * 100) : 0;

  const byConnColumns = [
    {
      key: "username",
      header: "Username",
      render: (row: { username: string; connections: number; octets: number }) => (
        <MonoValue>{row.username}</MonoValue>
      ),
    },
    {
      key: "connections",
      header: "Connections",
      render: (row: { username: string; connections: number; octets: number }) => (
        <MonoValue>{row.connections}</MonoValue>
      ),
    },
    {
      key: "traffic",
      header: "Traffic",
      render: (row: { username: string; connections: number; octets: number }) => (
        <MonoValue>{formatBytes(row.octets)}</MonoValue>
      ),
    },
  ];

  const byThroughputColumns = [
    {
      key: "username",
      header: "Username",
      render: (row: { username: string; connections: number; octets: number }) => (
        <MonoValue>{row.username}</MonoValue>
      ),
    },
    {
      key: "traffic",
      header: "Traffic",
      render: (row: { username: string; connections: number; octets: number }) => (
        <MonoValue>{formatBytes(row.octets)}</MonoValue>
      ),
    },
    {
      key: "connections",
      header: "Connections",
      render: (row: { username: string; connections: number; octets: number }) => (
        <MonoValue>{row.connections}</MonoValue>
      ),
    },
  ];

  return (
    <div className="flex flex-col gap-6 pt-2">
      {/* Routing split */}
      <div className="rounded-xs bg-bg-card p-4 flex flex-col gap-3">
        <div className="flex items-baseline justify-between">
          <FieldLabel>Total: {total.toLocaleString()}</FieldLabel>
          <FieldLabel>Active Users: {connections.activeUsers.toLocaleString()}</FieldLabel>
        </div>
        <ProgressBar
          value={mePct}
          label={`ME: ${connections.currentMe.toLocaleString()} (${mePct}%)`}
          size="sm"
          variant="info"
        />
        <ProgressBar
          value={directPct}
          label={`Direct: ${connections.currentDirect.toLocaleString()} (${directPct}%)`}
          size="sm"
          variant="info"
        />
        {connections.staleCacheUsed && <Badge variant="warn">⚠ Stale cache in use</Badge>}
      </div>

      {/* Top by connections */}
      {connections.topByConnections.length > 0 && (
        <div>
          <SectionHeader title="Top by Connections" />
          <DataTable
            columns={byConnColumns}
            data={connections.topByConnections}
            keyExtractor={(row) => row.username}
          />
        </div>
      )}

      {/* Top by throughput */}
      {connections.topByThroughput.length > 0 && (
        <div>
          <SectionHeader title="Top by Throughput" />
          <DataTable
            columns={byThroughputColumns}
            data={connections.topByThroughput}
            keyExtractor={(row) => row.username}
          />
        </div>
      )}

      {/* Lifetime stats */}
      <div className="rounded-xs bg-bg-card p-4 flex flex-col gap-2">
        <FieldLabel>Lifetime Stats</FieldLabel>
        <div className="grid grid-cols-2 gap-x-4 gap-y-1.5 text-sm">
          <span className="text-fg-muted">Total connections</span>
          <MonoValue>{summary.connectionsTotal.toLocaleString()}</MonoValue>
          <span className="text-fg-muted">Bad connections</span>
          <span className="font-mono text-xs text-fg">
            {summary.connectionsBadTotal.toLocaleString()}
            {summary.connectionsTotal > 0 && (
              <span className="text-fg-muted ml-1">
                ({((summary.connectionsBadTotal / summary.connectionsTotal) * 100).toFixed(1)}%)
              </span>
            )}
          </span>
          <span className="text-fg-muted">Handshake timeouts</span>
          <MonoValue>{summary.handshakeTimeoutsTotal.toLocaleString()}</MonoValue>
          <span className="text-fg-muted">Configured users</span>
          <MonoValue>{summary.configuredUsers.toLocaleString()}</MonoValue>
        </div>
      </div>
    </div>
  );
}
