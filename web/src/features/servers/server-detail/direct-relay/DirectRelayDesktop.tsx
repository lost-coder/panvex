import { AlertStrip, InitCard, SectionHeader } from "@/ui";
import type { ServerDetailPageProps } from "@/shared/api/types-pages/pages";

import { GatesPanel } from "../components/GatesPanel";
import { Fold } from "../components/Fold";
import { TimelineStrip } from "../components/TimelineStrip";
import { UpstreamsList } from "../components/UpstreamsList";
import type { AlertItem } from "../format";

import { FallbackBanner } from "./FallbackBanner";
import { UpstreamHealthCard } from "./UpstreamHealthCard";

export interface DirectRelayDesktopProps {
  server: ServerDetailPageProps["server"];
  initState: ServerDetailPageProps["initState"];
  alertItems: AlertItem[];
  metricsChart: ServerDetailPageProps["metricsChart"];
  fallback: { active: boolean; durationSeconds: number; escalated: boolean };
}

// Direct-mode desktop layout.
//
// Direct relay nodes have no DC fleet, no ME pool, no telemetry radar.
// We swap the ME-mode TelemetryCard for a slim TimelineStrip and the
// DC tiles for an UpstreamHealthCard headline plus the regular
// UpstreamsList rows. Field paths come from `server.upstreamSummary`
// (Phase 5 zod) — guarded with safe fallbacks because the type is
// optional on the page-props contract.
export function DirectRelayDesktop({
  server,
  initState,
  alertItems,
  metricsChart,
  fallback,
}: Readonly<DirectRelayDesktopProps>) {
  const summary = server.upstreamSummary;
  const healthy = summary?.healthyTotal ?? server.upstreams.filter((u) => u.healthy).length;
  const total = summary?.configuredTotal ?? server.upstreams.length;
  const failRatePct5m = summary?.failRatePct5m ?? 0;
  const failRateKnown = summary?.failRateKnown ?? false;
  const metricsPoints = metricsChart?.points ?? [];

  return (
    <div className="flex flex-col gap-4">
      {initState && <InitCard {...initState} />}

      {fallback.active && (
        <FallbackBanner
          durationSeconds={fallback.durationSeconds}
          escalated={fallback.escalated}
        />
      )}

      <UpstreamHealthCard
        healthy={healthy}
        total={total}
        failRatePct5m={failRatePct5m}
        failRateKnown={failRateKnown}
        currentDirectConnections={server.connections.currentDirect}
      />

      <section className="rounded-xs bg-bg-card border border-border p-4">
        <div className="flex items-center justify-between pb-2 border-b border-divider mb-3">
          <span className="text-[10px] font-mono uppercase tracking-wider text-fg-muted">
            Live telemetry{metricsChart?.timeRange ? ` · last ${metricsChart.timeRange}` : ""}
          </span>
        </div>
        <TimelineStrip metricsPoints={metricsPoints} events={[]} />
      </section>

      {alertItems.length > 0 && (
        <AlertStrip
          alerts={alertItems.map((a) => ({ severity: a.severity, message: a.message }))}
        />
      )}

      <section className="flex flex-col gap-2">
        <SectionHeader title="Upstreams" badge={server.upstreams.length} />
        <UpstreamsList upstreams={server.upstreams} />
      </section>

      <Fold title="Gates" rightHint="">
        <GatesPanel gates={server.gates} />
      </Fold>
    </div>
  );
}
