import { InitCard } from "@/ui";
import type { ServerDetailPageProps } from "@/shared/api/types-pages/pages";

import { GatesPanel } from "../components/GatesPanel";
import { TimelineStrip } from "../components/TimelineStrip";
import { UpstreamsList } from "../components/UpstreamsList";

import { FallbackBanner } from "./FallbackBanner";
import { UpstreamHealthCard } from "./UpstreamHealthCard";

export interface DirectRelayMobileProps {
  server: ServerDetailPageProps["server"];
  initState: ServerDetailPageProps["initState"];
  metricsChart: ServerDetailPageProps["metricsChart"];
  fallback: {
    active: boolean;
    durationSeconds: number;
    escalated: boolean;
    enteredAtUnix: number | null;
  };
}

// Direct-mode mobile layout. Same widgets as the desktop but stacked
// vertically; we drop the alert strip + Fold to keep the small-screen
// view focused on the data that matters in direct relay (upstream
// health, fallback state, gates summary).
export function DirectRelayMobile(props: Readonly<DirectRelayMobileProps>) {
  const { server, initState, metricsChart, fallback } = props;
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
          enteredAtUnix={fallback.enteredAtUnix}
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
        <TimelineStrip metricsPoints={metricsPoints} events={[]} />
      </section>
      <UpstreamsList upstreams={server.upstreams} />
      <GatesPanel gates={server.gates} />
    </div>
  );
}
