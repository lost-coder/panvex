import { useState } from "react";
import { FieldLabel, MonoValue } from "@/ui/primitives";
import { Badge } from "@/ui/primitives/Badge";
import { DataTable } from "@/ui/components/DataTable";
import type { ServerDetailPageProps, ServerUpstreamData } from "@/shared/api/types-pages/pages";

export function UpstreamsTab({ server }: Readonly<{ server: ServerDetailPageProps["server"] }>) {
  const { upstreams, upstreamSummary, upstreamZeroCounters } = server;
  const [showZeroCounters, setShowZeroCounters] = useState(false);

  const columns = [
    {
      key: "id",
      header: "#",
      render: (row: Readonly<ServerUpstreamData>) => (
        <span className="font-mono text-xs text-fg-muted">{row.upstreamId}</span>
      ),
      className: "w-10",
    },
    {
      key: "routeKind",
      header: "Type",
      render: (row: Readonly<ServerUpstreamData>) => <Badge variant="default">{row.routeKind}</Badge>,
    },
    {
      key: "address",
      header: "Address",
      render: (row: Readonly<ServerUpstreamData>) => <MonoValue>{row.address}</MonoValue>,
    },
    {
      key: "healthy",
      header: "Health",
      render: (row: Readonly<ServerUpstreamData>) => (
        <Badge variant={row.healthy ? "ok" : "error"}>{row.healthy ? "✓ OK" : "✗ FAIL"}</Badge>
      ),
    },
    {
      key: "fails",
      header: "Fails",
      render: (row: Readonly<ServerUpstreamData>) => (
        <span className={`font-mono text-xs ${row.fails > 0 ? "text-status-warn" : ""}`}>
          {row.fails}
        </span>
      ),
    },
    {
      key: "latency",
      header: "Latency",
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
      header: "Last Check",
      render: (row: Readonly<ServerUpstreamData>) => (
        <span className="font-mono text-xs text-fg-muted">{row.lastCheckAgeSecs}s ago</span>
      ),
    },
  ];

  return (
    <div className="flex flex-col gap-4 pt-2">
      {/* Summary badges */}
      {upstreamSummary && (
        <div className="flex flex-wrap gap-2">
          <Badge variant="default">Total: {upstreamSummary.configuredTotal}</Badge>
          <Badge variant="ok">✓ Healthy: {upstreamSummary.healthyTotal}</Badge>
          {upstreamSummary.unhealthyTotal > 0 && (
            <Badge variant="error">✗ Unhealthy: {upstreamSummary.unhealthyTotal}</Badge>
          )}
          {upstreamSummary.directTotal > 0 && (
            <Badge variant="default">Direct: {upstreamSummary.directTotal}</Badge>
          )}
          {upstreamSummary.socks5Total > 0 && (
            <Badge variant="default">SOCKS5: {upstreamSummary.socks5Total}</Badge>
          )}
        </div>
      )}

      <DataTable
        columns={columns}
        data={upstreams}
        keyExtractor={(row) => String(row.upstreamId)}
        emptyMessage="No upstreams configured"
      />

      {/* Connect Statistics (collapsible) */}
      {upstreamZeroCounters && (
        <div className="rounded-xs bg-bg-card overflow-hidden">
          <button
            onClick={() => setShowZeroCounters((v) => !v)}
            className="w-full px-4 py-3 flex items-center justify-between text-left hover:bg-bg-card-hover transition-colors"
          >
            <FieldLabel>Connect Statistics</FieldLabel>
            <span className="text-fg-muted text-xs select-none">
              {showZeroCounters ? "▾" : "›"}
            </span>
          </button>
          {showZeroCounters && (
            <div className="px-4 pb-4 grid grid-cols-2 md:grid-cols-4 gap-2">
              {[
                { label: "Attempt Total", value: upstreamZeroCounters.connectAttemptTotal },
                { label: "Success Total", value: upstreamZeroCounters.connectSuccessTotal },
                { label: "Fail Total", value: upstreamZeroCounters.connectFailTotal },
                {
                  label: "Failfast Hard Error",
                  value: upstreamZeroCounters.connectFailfastHardErrorTotal,
                },
                {
                  label: "Success ≤100ms",
                  value: upstreamZeroCounters.connectDurationSuccessBucketLe100ms,
                },
                {
                  label: "Success 101–500ms",
                  value: upstreamZeroCounters.connectDurationSuccessBucket101_500ms,
                },
                {
                  label: "Success 501–1000ms",
                  value: upstreamZeroCounters.connectDurationSuccessBucket501_1000ms,
                },
                {
                  label: "Success >1000ms",
                  value: upstreamZeroCounters.connectDurationSuccessBucketGt1000ms,
                },
                {
                  label: "Fail ≤100ms",
                  value: upstreamZeroCounters.connectDurationFailBucketLe100ms,
                },
                {
                  label: "Fail 101–500ms",
                  value: upstreamZeroCounters.connectDurationFailBucket101_500ms,
                },
                {
                  label: "Fail 501–1000ms",
                  value: upstreamZeroCounters.connectDurationFailBucket501_1000ms,
                },
                {
                  label: "Fail >1000ms",
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
