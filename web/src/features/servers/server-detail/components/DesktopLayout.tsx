import { AlertStrip, InitCard, SectionHeader } from "@/ui";
import type { ServerDcData, ServerDetailPageProps } from "@/shared/api/types-pages/pages";

import { PulseGrid, type PulseTickData } from "./PulseGrid";
import { TelemetryCard } from "./TelemetryCard";
import { DcTiles } from "./DcTiles";
import { GatesUpstreamsCard } from "./GatesUpstreamsCard";
import { Fold } from "./Fold";
import type { AlertItem, TimelineEvent } from "../format";

/**
 * Desktop story for the server detail page: handoff-style vertical
 * stack without tabs. Renders init card, KPI pulse ribbon, telemetry
 * card, alerts, DC tile grid, gates+upstreams card, and the legacy
 * fold panels (ME pool, top clients, events).
 */
export function DesktopLayout({
  server,
  initState,
  pulseItems,
  sortedDcs,
  dcOk,
  dcWarn,
  dcErr,
  metricsChart,
  timelineEvents,
  alertItems,
  mePoolContent,
  connectionsContent,
  eventsContent,
  onSelectDc,
}: {
  server: ServerDetailPageProps["server"];
  initState: ServerDetailPageProps["initState"];
  pulseItems: PulseTickData[];
  sortedDcs: ServerDcData[];
  dcOk: number;
  dcWarn: number;
  dcErr: number;
  metricsChart: ServerDetailPageProps["metricsChart"];
  timelineEvents: TimelineEvent[];
  alertItems: AlertItem[];
  mePoolContent: React.ReactNode;
  connectionsContent: React.ReactNode;
  eventsContent: React.ReactNode;
  onSelectDc: (dc: ServerDcData) => void;
}) {
  return (
    <div className="hidden md:flex flex-col gap-6">
      {initState && <InitCard {...initState} />}

      {/* Pulse row — 4 metrics as tickers in a 4-col ribbon.
          Hint strings fold in the context that used to live inside
          the separate "Connections detail" fold (routing split,
          lifetime totals, configured users). */}
      <PulseGrid variant="desktop" items={pulseItems} />

      <TelemetryCard
        sortedDcs={sortedDcs}
        dcOk={dcOk}
        dcWarn={dcWarn}
        dcErr={dcErr}
        metricsChart={metricsChart}
        timelineEvents={timelineEvents}
        onSelectDc={onSelectDc}
      />

      {alertItems.length > 0 && <AlertStrip alerts={alertItems} />}

      {/* DC tiles grid — problem-first ordering already applied. */}
      <section className="flex flex-col gap-2">
        <div className="flex items-center justify-between">
          <SectionHeader title="Data Centers" badge={sortedDcs.length} />
          <span className="text-[10px] font-mono text-fg-muted">
            sorted by coverage · worst first
          </span>
        </div>
        <DcTiles dcs={sortedDcs} onSelect={onSelectDc} />
      </section>

      <GatesUpstreamsCard gates={server.gates} upstreams={server.upstreams} />

      {/* Folds — previously tabs. Reuse the existing tab panels so
          we don't lose any data surface during the rework. */}
      {server.mePool?.enabled && (
        <Fold
          title="ME Pool"
          rightHint={`${server.mePool.summary.aliveWriters}/${server.mePool.summary.requiredWriters} writers alive`}
        >
          {mePoolContent}
        </Fold>
      )}
      {/* Top clients — keeps the per-user breakdown that used to
          sit inside Connections detail, minus the routing/lifetime
          numbers that now live in the hero pulse row. */}
      {(server.connections.topByConnections.length > 0 ||
        server.connections.topByThroughput.length > 0) && (
        <Fold
          title="Top clients"
          rightHint={`${server.connections.topByConnections.length} by conn · ${server.connections.topByThroughput.length} by traffic`}
          defaultOpen={false}
        >
          {connectionsContent}
        </Fold>
      )}
      <Fold
        title="Events"
        rightHint={`${server.events.length} entries${server.eventsDroppedTotal ? ` · ${server.eventsDroppedTotal} dropped` : ""}`}
        defaultOpen={false}
      >
        {eventsContent}
      </Fold>
    </div>
  );
}
