import { useState } from "react";
import { useTranslation } from "react-i18next";

import { FieldLabel, MonoValue } from "@/ui/primitives";
import { Badge } from "@/ui/primitives/Badge";
import { DataTable } from "@/ui/components/DataTable";
import type { ServerDetailPageProps, ServerUpstreamData } from "@/shared/api/types-pages/pages";

export function UpstreamsTab({ server }: Readonly<{ server: ServerDetailPageProps["server"] }>) {
  const { t } = useTranslation("servers");
  const { upstreams, upstreamSummary, upstreamZeroCounters } = server;
  const [showZeroCounters, setShowZeroCounters] = useState(false);

  const columns = [
    {
      key: "id",
      header: t("detail.upstreamsTab.id"),
      render: (row: Readonly<ServerUpstreamData>) => (
        <span className="font-mono text-xs text-fg-muted">{row.upstreamId}</span>
      ),
      className: "w-10",
    },
    {
      key: "routeKind",
      header: t("detail.upstreamsTab.type"),
      render: (row: Readonly<ServerUpstreamData>) => <Badge variant="default">{row.routeKind}</Badge>,
    },
    {
      key: "address",
      header: t("detail.upstreamsTab.address"),
      render: (row: Readonly<ServerUpstreamData>) => <MonoValue>{row.address}</MonoValue>,
    },
    {
      key: "healthy",
      header: t("detail.upstreamsTab.health"),
      render: (row: Readonly<ServerUpstreamData>) => (
        <Badge variant={row.healthy ? "ok" : "error"}>
          {row.healthy ? t("detail.upstreamsTab.ok") : t("detail.upstreamsTab.fail")}
        </Badge>
      ),
    },
    {
      key: "fails",
      header: t("detail.upstreamsTab.fails"),
      render: (row: Readonly<ServerUpstreamData>) => (
        <span className={`font-mono text-xs ${row.fails > 0 ? "text-status-warn" : ""}`}>
          {row.fails}
        </span>
      ),
    },
    {
      key: "latency",
      header: t("detail.upstreamsTab.latency"),
      render: (row: Readonly<ServerUpstreamData>) => (
        <MonoValue>
          {row.effectiveLatencyMs != null && row.effectiveLatencyMs > 0
            ? `${row.effectiveLatencyMs}ms`
            : "—"}
        </MonoValue>
      ),
    },
    {
      key: "lastCheck",
      header: t("detail.upstreamsTab.lastCheck"),
      render: (row: Readonly<ServerUpstreamData>) => (
        <span className="font-mono text-xs text-fg-muted">
          {t("detail.upstreamsTab.lastCheckAgo", { seconds: row.lastCheckAgeSecs })}
        </span>
      ),
    },
  ];

  return (
    <div className="flex flex-col gap-4 pt-2">
      {/* Summary badges */}
      {upstreamSummary && (
        <div className="flex flex-wrap gap-2">
          <Badge variant="default">{t("detail.upstreamsTab.total", { count: upstreamSummary.configuredTotal })}</Badge>
          <Badge variant="ok">{t("detail.upstreamsTab.healthy", { count: upstreamSummary.healthyTotal })}</Badge>
          {upstreamSummary.unhealthyTotal > 0 && (
            <Badge variant="error">{t("detail.upstreamsTab.unhealthy", { count: upstreamSummary.unhealthyTotal })}</Badge>
          )}
          {upstreamSummary.directTotal > 0 && (
            <Badge variant="default">{t("detail.upstreamsTab.direct", { count: upstreamSummary.directTotal })}</Badge>
          )}
          {upstreamSummary.socks5Total > 0 && (
            <Badge variant="default">{t("detail.upstreamsTab.socks5", { count: upstreamSummary.socks5Total })}</Badge>
          )}
        </div>
      )}

      <DataTable
        columns={columns}
        data={upstreams}
        keyExtractor={(row) => String(row.upstreamId)}
        emptyMessage={t("detail.upstreamsTab.noUpstreams")}
      />

      {/* Connect Statistics (collapsible) */}
      {upstreamZeroCounters && (
        <div className="rounded-xs bg-bg-card overflow-hidden">
          <button
            onClick={() => setShowZeroCounters((v) => !v)}
            className="w-full px-4 py-3 flex items-center justify-between text-left hover:bg-bg-card-hover transition-colors"
          >
            <FieldLabel>{t("detail.upstreamsTab.connectStats")}</FieldLabel>
            <span className="text-fg-muted text-xs select-none">
              {showZeroCounters ? "▾" : "›"}
            </span>
          </button>
          {showZeroCounters && (
            <div className="px-4 pb-4 grid grid-cols-2 md:grid-cols-4 gap-2">
              {[
                { label: t("detail.upstreamsTab.attemptTotal"), value: upstreamZeroCounters.connectAttemptTotal },
                { label: t("detail.upstreamsTab.successTotal"), value: upstreamZeroCounters.connectSuccessTotal },
                { label: t("detail.upstreamsTab.failTotal"), value: upstreamZeroCounters.connectFailTotal },
                {
                  label: t("detail.upstreamsTab.failfastHardError"),
                  value: upstreamZeroCounters.connectFailfastHardErrorTotal,
                },
                {
                  label: t("detail.upstreamsTab.successLe100"),
                  value: upstreamZeroCounters.connectDurationSuccessBucketLe100ms,
                },
                {
                  label: t("detail.upstreamsTab.success101_500"),
                  value: upstreamZeroCounters.connectDurationSuccessBucket101_500ms,
                },
                {
                  label: t("detail.upstreamsTab.success501_1000"),
                  value: upstreamZeroCounters.connectDurationSuccessBucket501_1000ms,
                },
                {
                  label: t("detail.upstreamsTab.successGt1000"),
                  value: upstreamZeroCounters.connectDurationSuccessBucketGt1000ms,
                },
                {
                  label: t("detail.upstreamsTab.failLe100"),
                  value: upstreamZeroCounters.connectDurationFailBucketLe100ms,
                },
                {
                  label: t("detail.upstreamsTab.fail101_500"),
                  value: upstreamZeroCounters.connectDurationFailBucket101_500ms,
                },
                {
                  label: t("detail.upstreamsTab.fail501_1000"),
                  value: upstreamZeroCounters.connectDurationFailBucket501_1000ms,
                },
                {
                  label: t("detail.upstreamsTab.failGt1000"),
                  value: upstreamZeroCounters.connectDurationFailBucketGt1000ms,
                },
              ].map(({ label, value }) => (
                <div key={label} className="rounded-xs bg-bg-hover p-2 flex flex-col gap-0.5">
                  <span className="text-base font-mono font-semibold text-fg leading-none">
                    {value.toLocaleString()}
                  </span>
                  <span className="text-[10px] text-fg-muted uppercase tracking-wider leading-none">
                    {label}
                  </span>
                </div>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
}
