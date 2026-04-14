import { useState, useMemo, useEffect } from "react";
import { ServerDetailPage, Spinner } from "@lost-coder/panvex-ui";
import { ErrorState } from "@/components/ErrorState";
import type { MetricsPoint } from "@lost-coder/panvex-ui";
import { useServerDetail } from "@/hooks/useServerDetail";
import { useServerMutations } from "@/hooks/useServerMutations";
import { useServerLoadHistory } from "@/hooks/useServerHistory";
import { useNavigate, useParams } from "@tanstack/react-router";
import { transformAgentConnection } from "@/lib/transforms/servers";

const RANGE_HOURS: Record<string, number> = { "1h": 1, "6h": 6, "24h": 24, "7d": 168 };

function toMetricsPoints(
  points: { CapturedAt: string; CPUPctAvg: number; CPUPctMax: number; MemPctAvg: number; MemPctMax: number; DiskPctAvg: number; DiskPctMax: number; Load1M: number; ConnectionsAvg: number; ConnectionsMax: number; ActiveUsersAvg: number; ActiveUsersMax: number; DCCoverageMinPct: number; NetBytesSent: number; NetBytesRecv: number }[]
): MetricsPoint[] {
  return points.map((p, i) => {
    let netUploadMbps = 0;
    let netDownloadMbps = 0;
    if (i > 0) {
      const prev = points[i - 1];
      const dtSec = (new Date(p.CapturedAt).getTime() - new Date(prev.CapturedAt).getTime()) / 1000;
      if (dtSec > 0) {
        netUploadMbps = ((p.NetBytesSent - prev.NetBytesSent) * 8) / dtSec / 1_000_000;
        netDownloadMbps = ((p.NetBytesRecv - prev.NetBytesRecv) * 8) / dtSec / 1_000_000;
      }
      if (netUploadMbps < 0) netUploadMbps = 0;
      if (netDownloadMbps < 0) netDownloadMbps = 0;
    }
    return {
      t: p.CapturedAt,
      cpuAvg: p.CPUPctAvg,
      cpuMax: p.CPUPctMax,
      memAvg: p.MemPctAvg,
      memMax: p.MemPctMax,
      diskAvg: p.DiskPctAvg,
      diskMax: p.DiskPctMax,
      connectionsAvg: p.ConnectionsAvg,
      connectionsMax: p.ConnectionsMax,
      activeUsersAvg: p.ActiveUsersAvg,
      activeUsersMax: p.ActiveUsersMax,
      dcCoverageMin: p.DCCoverageMinPct,
      load1m: p.Load1M,
      netUploadMbps,
      netDownloadMbps,
    };
  });
}

export function ServerDetailContainer() {
  const { serverId } = useParams({ strict: false });
  const { server, initState, lastUpdatedAt, raw, isLoading, error } = useServerDetail(serverId ?? "");
  const {
    allowCertRecoveryMutation,
    revokeCertRecoveryMutation,
    boostDetailMutation,
    renameMutation,
    deregisterMutation,
  } = useServerMutations(serverId ?? "");
  const navigate = useNavigate();
  const [timeRange, setTimeRange] = useState("6h");

  const hours = RANGE_HOURS[timeRange] ?? 6;
  // Truncate to the minute so the query key stays stable between renders.
  // Re-derived every minute via the interval; useMemo only dedupes within
  // the same render cycle so the dependency on `hours` is sufficient.
  const [nowMinute, setNowMinute] = useState(() => Math.floor(Date.now() / 60_000) * 60_000);
  useEffect(() => {
    const id = setInterval(() => setNowMinute(Math.floor(Date.now() / 60_000) * 60_000), 60_000);
    return () => clearInterval(id);
  }, []);
  const from = useMemo(() => {
    return new Date(nowMinute - hours * 3600_000).toISOString();
  }, [hours, nowMinute]);
  const { points: rawPoints, resolution } = useServerLoadHistory(serverId ?? "", from);
  const metricsPoints = useMemo(() => toMetricsPoints(rawPoints as any[]), [rawPoints]);

  if (isLoading || !server) {
    return <div className="flex items-center justify-center h-64"><Spinner /></div>;
  }

  if (error) {
    return <ErrorState message={error.message} onRetry={() => window.location.reload()} />;
  }

  return (
    <ServerDetailPage
      server={server}
      initState={initState}
      lastUpdatedAt={lastUpdatedAt}
      onBack={() => navigate({ to: "/servers" })}
      onBoostDetail={() => boostDetailMutation.mutate()}
      agentConnection={transformAgentConnection(raw?.server?.agent)}
      onAllowReEnrollment={() => allowCertRecoveryMutation.mutate()}
      onRevokeGrant={() => revokeCertRecoveryMutation.mutate()}
      onRename={(name: string) => renameMutation.mutate(name)}
      onDeregister={() => {
        deregisterMutation.mutate(undefined, {
          onSuccess: () => navigate({ to: "/servers" }),
        });
      }}
      metricsChart={{
        points: metricsPoints,
        resolution,
        timeRange,
        onTimeRangeChange: setTimeRange,
      }}
    />
  );
}
