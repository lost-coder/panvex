// Q5.U-Q-08: extracted from ClientDetailPage.tsx (limits + metadata
// card). Reads only from the client-level payload; no shared state
// with the page so a clean component move was sufficient.
//
// Limits are configured per-Telemt-node (the panel pushes the same
// value to every assigned agent). When the client is deployed to N
// nodes the *effective fleet limit* is therefore `entered × N`. We
// surface both numbers so operators are not surprised when their
// dashboard shows 200 active conns out of a 50 "limit" — usage is
// summed across nodes, the limit is per-node.
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";

import { Badge, KvGrid, MonoValue, formatQuota } from "@/ui";
import type { ClientDetailPageProps } from "@/shared/api/types-pages/pages";

export function LimitsCard({ client }: Readonly<{ client: ClientDetailPageProps["client"] }>) {
  const { t } = useTranslation("clients");
  const nodes = Math.max(1, client.deployments.length);

  const renderCountLimit = (perNode: number): ReactNode => {
    if (perNode <= 0) {
      return <MonoValue>{t("limits.unlimited")}</MonoValue>;
    }
    if (nodes <= 1) {
      return <MonoValue>{perNode}</MonoValue>;
    }
    return (
      <span className="flex items-baseline gap-1.5 flex-wrap">
        <MonoValue>{perNode * nodes}</MonoValue>
        <span className="text-[11px] font-mono text-fg-muted">
          {t("limits.perNodeSuffix", { per: perNode, count: nodes })}
        </span>
      </span>
    );
  };

  const renderQuota = (): ReactNode => {
    if (client.dataQuotaBytes <= 0) {
      return <MonoValue>{formatQuota(client.dataQuotaBytes)}</MonoValue>;
    }
    if (nodes <= 1) {
      return <MonoValue>{formatQuota(client.dataQuotaBytes)}</MonoValue>;
    }
    return (
      <span className="flex items-baseline gap-1.5 flex-wrap">
        <MonoValue>{formatQuota(client.dataQuotaBytes * nodes)}</MonoValue>
        <span className="text-[11px] font-mono text-fg-muted">
          {t("limits.perNodeSuffix", { per: formatQuota(client.dataQuotaBytes), count: nodes })}
        </span>
      </span>
    );
  };

  return (
    <section className="rounded-xs bg-bg-card border border-divider p-4 flex flex-col gap-3">
      <span className="text-sm font-semibold text-fg">{t("limits.title")}</span>
      <KvGrid
        rows={[
          {
            label: t("limits.adTag"),
            value: client.userAdTag ? (
              <MonoValue>{client.userAdTag}</MonoValue>
            ) : (
              <span className="text-xs text-fg-faint">—</span>
            ),
          },
          {
            label: t("limits.maxTcpConns"),
            value: renderCountLimit(client.maxTcpConns),
          },
          {
            label: t("limits.maxUniqueIps"),
            value: renderCountLimit(client.maxUniqueIps),
          },
          {
            label: t("limits.quota"),
            value: renderQuota(),
          },
          {
            label: t("limits.fleetGroups"),
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
