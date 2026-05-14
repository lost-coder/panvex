import { useTranslation } from "react-i18next";

import { MonoValue } from "@/ui/primitives";
import { SectionHeader } from "@/ui/layout/SectionHeader";
import { Badge } from "@/ui/primitives/Badge";
import { DataTable } from "@/ui/components/DataTable";
import { formatBytes } from "@/ui/lib/format";
import type { ServerDetailPageProps } from "@/shared/api/types-pages/pages";

/**
 * Top-clients view — "Connections detail" used to duplicate the routing
 * split and lifetime counters that now live in the hero pulse row. What
 * remains unique here are the per-user tables, so the panel is scoped
 * to that plus a `staleCacheUsed` warning.
 */
export function ConnectionsTab({ server }: Readonly<{ server: ServerDetailPageProps["server"] }>) {
  const { t } = useTranslation("servers");
  const { connections } = server;

  const byConnColumns = [
    {
      key: "username",
      header: t("detail.topClients.username"),
      render: (row: { username: string; connections: number; octets: number }) => (
        <MonoValue>{row.username}</MonoValue>
      ),
    },
    {
      key: "connections",
      header: t("detail.topClients.connections"),
      render: (row: { username: string; connections: number; octets: number }) => (
        <MonoValue>{row.connections}</MonoValue>
      ),
    },
    {
      key: "traffic",
      header: t("detail.topClients.traffic"),
      render: (row: { username: string; connections: number; octets: number }) => (
        <MonoValue>{formatBytes(row.octets)}</MonoValue>
      ),
    },
  ];

  const byThroughputColumns = [
    {
      key: "username",
      header: t("detail.topClients.username"),
      render: (row: { username: string; connections: number; octets: number }) => (
        <MonoValue>{row.username}</MonoValue>
      ),
    },
    {
      key: "traffic",
      header: t("detail.topClients.traffic"),
      render: (row: { username: string; connections: number; octets: number }) => (
        <MonoValue>{formatBytes(row.octets)}</MonoValue>
      ),
    },
    {
      key: "connections",
      header: t("detail.topClients.connections"),
      render: (row: { username: string; connections: number; octets: number }) => (
        <MonoValue>{row.connections}</MonoValue>
      ),
    },
  ];

  const hasData =
    connections.topByConnections.length > 0 || connections.topByThroughput.length > 0;

  return (
    <div className="flex flex-col gap-5">
      {connections.staleCacheUsed && (
        <Badge variant="warn">{t("detail.topClients.staleCache")}</Badge>
      )}

      {!hasData && (
        <div className="py-6 text-center text-sm text-fg-muted">
          {t("detail.topClients.noClientActivity")}
        </div>
      )}

      {connections.topByConnections.length > 0 && (
        <div className="flex flex-col gap-2">
          <SectionHeader title={t("detail.topClients.topByConnections")} />
          <DataTable
            columns={byConnColumns}
            data={connections.topByConnections}
            keyExtractor={(row) => `conn-${row.username}`}
          />
        </div>
      )}

      {connections.topByThroughput.length > 0 && (
        <div className="flex flex-col gap-2">
          <SectionHeader title={t("detail.topClients.topByThroughput")} />
          <DataTable
            columns={byThroughputColumns}
            data={connections.topByThroughput}
            keyExtractor={(row) => `tp-${row.username}`}
          />
        </div>
      )}
    </div>
  );
}
