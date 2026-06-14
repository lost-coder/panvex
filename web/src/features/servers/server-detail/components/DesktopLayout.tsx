import { useTranslation } from "react-i18next";

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
}: Readonly<{
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
  onSelectDc: (dc: Readonly<ServerDcData>) => void;
}>) {
  const { t } = useTranslation("servers");
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
          <SectionHeader title={t("detail.dataCenters.title")} badge={sortedDcs.length} />
          <span className="text-nano font-mono text-fg-muted">
            {t("detail.dataCenters.sortedHint")}
          </span>
        </div>
        <DcTiles dcs={sortedDcs} onSelect={onSelectDc} />
      </section>

      {/* DesktopLayout is rendered only by the ME branch of the server
          detail dispatcher, so the gates panel always uses the ME row
          set. Direct/Fallback get their own GatesPanel mount via the
          DirectRelay variants. */}
      <GatesUpstreamsCard gates={server.gates} upstreams={server.upstreams} mode="me" />

      {/* Folds — previously tabs. Reuse the existing tab panels so
          we don't lose any data surface during the rework. */}
      {server.mePool?.enabled && (
        <Fold
          title={t("detail.folds.mePool")}
          rightHint={t("detail.folds.mePoolHint", {
            alive: server.mePool.summary.aliveWriters,
            required: server.mePool.summary.requiredWriters,
          })}
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
          title={t("detail.folds.topClients")}
          rightHint={t("detail.folds.topClientsHint", {
            connCount: server.connections.topByConnections.length,
            trafficCount: server.connections.topByThroughput.length,
          })}
          defaultOpen={false}
        >
          {connectionsContent}
        </Fold>
      )}
      <Fold
        title={t("detail.folds.events")}
        rightHint={
          server.eventsDroppedTotal
            ? t("detail.folds.eventsHintDropped", {
                count: server.events.length,
                dropped: server.eventsDroppedTotal,
              })
            : t("detail.folds.eventsHint", { count: server.events.length })
        }
        defaultOpen={false}
      >
        {eventsContent}
      </Fold>
    </div>
  );
}
