import { useCallback, useMemo, useState } from "react";

import {
  Breadcrumbs,
  PageHeader,
  StatusBeacon,
  formatUptime,
} from "@/ui";
import { AgentConnectionSection } from "@/features/servers/ui/AgentConnectionSection";
import type { ServerDetailPageProps, ServerDcData } from "@/shared/api/types-pages/pages";

import { useRelativeTime } from "./useRelativeTime";
import { ServerActionsDropdown } from "./ServerActionsDropdown";
import { RelativeTimeBadge } from "./components/RelativeTimeBadge";
import { ServerHero } from "./components/ServerHero";
import { MobileLayout } from "./components/MobileLayout";
import { DesktopLayout } from "./components/DesktopLayout";
import { GatesPanel } from "./components/GatesPanel";
import { UpstreamsList } from "./components/UpstreamsList";
import { DcDetailSheet } from "./components/DcDetailSheet";
import { RenameDialog } from "./components/RenameDialog";
import { DeregisterDialog } from "./components/DeregisterDialog";
import type { PulseTickData } from "./components/PulseGrid";
import {
  computeAlertItems,
  computeBadRate,
  computeCoverageStats,
  statusSentence,
  toDcStripItems,
  toTimelineEvents,
} from "./format";
import { ConnectionsTab } from "./tabs/ConnectionsTab";
import { MePoolTab } from "./tabs/MePoolTab";
import { EventsTab } from "./tabs/EventsTab";
import { ServerDetailProvider } from "./ServerDetailContext";

const noop = () => {};

// ─── Main page ────────────────────────────────────────────────────────
export function ServerDetailPage({
  server,
  onBack,
  onReload,
  onBoostDetail,
  initState,
  lastUpdatedAt,
  agentConnection,
  onAllowReEnrollment,
  onRevokeGrant,
  onRename,
  onDeregister,
  metricsChart,
}: ServerDetailPageProps) {
  const { label: relativeTime, stale: relativeTimeStale } = useRelativeTime(lastUpdatedAt);
  const { systemInfo, gates, connections, summary, dcs } = server;

  const [selectedDc, setSelectedDc] = useState<ServerDcData | null>(null);
  const [renameOpen, setRenameOpen] = useState(false);
  const [deregisterOpen, setDeregisterOpen] = useState(false);

  // ─── Derived data — memoised so child components that take props
  //     can be wrapped in React.memo without false-positive re-renders. ──
  const sortedDcs = useMemo(
    () => [...dcs].sort((a, b) => a.coveragePct - b.coveragePct),
    [dcs],
  );
  const { minCoverage, avgCoverage, dcOk, dcWarn, dcErr } = useMemo(
    () => computeCoverageStats(sortedDcs),
    [sortedDcs],
  );
  const badRate = useMemo(
    () => computeBadRate(summary.connectionsBadTotal, summary.connectionsTotal),
    [summary.connectionsBadTotal, summary.connectionsTotal],
  );
  const pulseWord = useMemo(
    () => statusSentence(server.status, sortedDcs.length, dcWarn, dcErr),
    [server.status, sortedDcs.length, dcWarn, dcErr],
  );
  const dcItems = useMemo(() => toDcStripItems(sortedDcs), [sortedDcs]);
  const alertItems = useMemo(
    () => computeAlertItems(sortedDcs, gates, Boolean(initState)),
    [sortedDcs, gates, initState],
  );
  const timelineEvents = useMemo(() => toTimelineEvents(server.events), [server.events]);

  // Mobile subtitle — same status sentence the desktop hero uses, plus
  // compact meta (version + uptime + optional config reload count).
  const subtitle = useMemo(
    () =>
      [
        pulseWord.toLowerCase(),
        `v${systemInfo.version}`,
        `up ${formatUptime(systemInfo.uptimeSeconds)}`,
        systemInfo.configReloadCount > 0 ? `${systemInfo.configReloadCount} reloads` : null,
      ]
        .filter(Boolean)
        .join(" · "),
    [pulseWord, systemInfo.version, systemInfo.uptimeSeconds, systemInfo.configReloadCount],
  );

  // Desktop pulse ribbon — full hint strings, 4-col layout.
  const desktopPulseItems = useMemo<PulseTickData[]>(
    () => [
      {
        label: "Connections",
        value: connections.current.toLocaleString(),
        hint: `${connections.currentMe.toLocaleString()} ME · ${connections.currentDirect.toLocaleString()} direct · total ${summary.connectionsTotal.toLocaleString()}`,
      },
      {
        label: "Active users",
        value: connections.activeUsers.toLocaleString(),
        hint: `of ${summary.configuredUsers.toLocaleString()} configured`,
      },
      {
        label: "Bad rate",
        value: `${badRate.toFixed(2)}%`,
        hint: `${summary.connectionsBadTotal.toLocaleString()} bad / ${summary.connectionsTotal.toLocaleString()} total`,
        tone: badRate > 5 ? "error" : badRate > 1 ? "warn" : "default",
      },
      {
        label: "DC coverage",
        value: avgCoverage,
        unit: "%",
        hint: `min ${minCoverage}% · ${dcOk} ok · ${dcWarn} warn · ${dcErr} err`,
        tone: avgCoverage < 95 ? "error" : avgCoverage < 100 ? "warn" : "ok",
      },
    ],
    [
      connections.current,
      connections.currentMe,
      connections.currentDirect,
      connections.activeUsers,
      summary.connectionsTotal,
      summary.configuredUsers,
      summary.connectionsBadTotal,
      badRate,
      avgCoverage,
      minCoverage,
      dcOk,
      dcWarn,
      dcErr,
    ],
  );

  // Mobile pulse 2×2 — shorter hint strings to fit narrow cells.
  const mobilePulseItems = useMemo<PulseTickData[]>(
    () => [
      {
        label: "Connections",
        value: connections.current.toLocaleString(),
        hint: `${connections.currentMe.toLocaleString()} ME · ${connections.currentDirect.toLocaleString()} direct`,
      },
      {
        label: "Active users",
        value: connections.activeUsers.toLocaleString(),
        hint: `of ${summary.configuredUsers.toLocaleString()}`,
      },
      {
        label: "Bad rate",
        value: `${badRate.toFixed(2)}%`,
        hint: `${summary.connectionsBadTotal.toLocaleString()} bad`,
        tone: badRate > 5 ? "error" : badRate > 1 ? "warn" : "default",
      },
      {
        label: "DC coverage",
        value: avgCoverage,
        unit: "%",
        hint: `min ${minCoverage}% · ${dcOk}/${dcWarn}/${dcErr}`,
        tone: avgCoverage < 95 ? "error" : avgCoverage < 100 ? "warn" : "ok",
      },
    ],
    [
      connections.current,
      connections.currentMe,
      connections.currentDirect,
      connections.activeUsers,
      summary.configuredUsers,
      summary.connectionsBadTotal,
      badRate,
      avgCoverage,
      minCoverage,
      dcOk,
      dcWarn,
      dcErr,
    ],
  );

  const connectionsContent = useMemo(() => <ConnectionsTab server={server} />, [server]);
  const mePoolContent = useMemo(() => <MePoolTab server={server} />, [server]);
  const eventsContent = useMemo(() => <EventsTab server={server} />, [server]);

  // Mobile tab content for gates + upstreams — mirrors the desktop
  // "one card, two columns" composition but stacks vertically so it
  // reads well in a narrow swipe pane.
  const gatesUpstreamsContent = useMemo(
    () => (
      <div className="flex flex-col gap-4">
        <div className="flex flex-col gap-2">
          <span className="text-sm font-semibold text-fg">Gates</span>
          <GatesPanel gates={gates} />
        </div>
        <div className="flex flex-col gap-2">
          <div className="flex items-center justify-between">
            <span className="text-sm font-semibold text-fg">Upstreams</span>
            <span className="text-[10px] font-mono text-fg-muted">
              {server.upstreams.length} peer{server.upstreams.length === 1 ? "" : "s"}
            </span>
          </div>
          <UpstreamsList upstreams={server.upstreams} />
        </div>
      </div>
    ),
    [gates, server.upstreams],
  );

  // Diagnostics tab removed — version + reload counts now ride in the
  // PageHeader subtitle. Upstreams tab is folded into "Gates &
  // Upstreams" so the mobile flow mirrors the desktop card.
  const mobileTabs = useMemo(
    () => [
      { id: "connections", label: "Top clients", content: connectionsContent },
      { id: "me-pool", label: "ME Pool", content: mePoolContent },
      { id: "gates", label: "Gates & Upstreams", content: gatesUpstreamsContent },
      { id: "events", label: "Events", content: eventsContent },
    ],
    [connectionsContent, mePoolContent, gatesUpstreamsContent, eventsContent],
  );

  // Stable handlers so the dropdowns/sheets don't churn on every parent
  // re-render. The page's render cost is dominated by the data widgets
  // below, not by these callbacks.
  const handleSelectDc = useCallback((dc: ServerDcData) => setSelectedDc(dc), []);
  const handleCloseDc = useCallback(() => setSelectedDc(null), []);
  const handleOpenRename = useCallback(
    () => (onRename ? setRenameOpen(true) : undefined),
    [onRename],
  );
  const handleOpenDeregister = useCallback(
    () => (onDeregister ? setDeregisterOpen(true) : undefined),
    [onDeregister],
  );
  const handleCloseDeregister = useCallback(() => setDeregisterOpen(false), []);

  return (
    <ServerDetailProvider server={server} serverId={server.id}>
      <div className="px-4 md:px-8 pt-3 pb-3">
        <Breadcrumbs items={[{ label: "Servers", onClick: onBack }, { label: server.name }]} />
      </div>

      {/* Desktop: no PageHeader — the hero pulse-bar inside the page body
          carries name, status and actions, so a separate header would
          just duplicate the title. Mobile still gets PageHeader so the
          sticky app-bar stays populated. */}
      <div className="md:hidden">
        <PageHeader
          title={server.name}
          subtitle={subtitle}
          trailing={
            <div className="flex items-center gap-3">
              {relativeTime && (
                <RelativeTimeBadge label={relativeTime} stale={relativeTimeStale} />
              )}
              <StatusBeacon status={server.status} size="xs" />
              <ServerActionsDropdown
                onReload={onReload}
                onBoostDetail={onBoostDetail}
                onRename={onRename ? handleOpenRename : undefined}
                onDeregister={onDeregister ? handleOpenDeregister : undefined}
              />
            </div>
          }
        />
      </div>

      <ServerHero
        server={server}
        pulseWord={pulseWord}
        relativeTime={relativeTime}
        relativeTimeStale={relativeTimeStale}
        onReload={onReload}
        onBoostDetail={onBoostDetail}
        onRename={onRename ? handleOpenRename : undefined}
        onDeregister={onDeregister ? handleOpenDeregister : undefined}
      />

      <div className="px-4 md:px-8 flex flex-col gap-6 pb-8 pt-6">
        <MobileLayout
          initState={initState}
          pulseItems={mobilePulseItems}
          alertItems={alertItems}
          metricsChart={metricsChart}
          sortedDcs={sortedDcs}
          dcItems={dcItems}
          mobileTabs={mobileTabs}
          onSelectDc={handleSelectDc}
        />

        <DesktopLayout
          server={server}
          initState={initState}
          pulseItems={desktopPulseItems}
          sortedDcs={sortedDcs}
          dcOk={dcOk}
          dcWarn={dcWarn}
          dcErr={dcErr}
          metricsChart={metricsChart}
          timelineEvents={timelineEvents}
          alertItems={alertItems}
          mePoolContent={mePoolContent}
          connectionsContent={connectionsContent}
          eventsContent={eventsContent}
          onSelectDc={handleSelectDc}
        />

        {agentConnection && (
          <AgentConnectionSection
            data={agentConnection}
            onAllowReEnrollment={onAllowReEnrollment ?? noop}
            onRevokeGrant={onRevokeGrant ?? noop}
          />
        )}
      </div>

      {/* Shared DC detail sheet — opens from mobile strip, desktop radar, and desktop tiles. */}
      <DcDetailSheet selectedDc={selectedDc} onClose={handleCloseDc} />

      <RenameDialog
        open={renameOpen}
        onOpenChange={setRenameOpen}
        currentName={server.name}
        onRename={onRename}
      />

      <DeregisterDialog
        open={deregisterOpen}
        onClose={handleCloseDeregister}
        onConfirm={onDeregister}
      />
    </ServerDetailProvider>
  );
}
