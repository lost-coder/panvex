import { lazy, Suspense, useCallback, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";

import {
  Breadcrumbs,
  NodeStateBadge,
  PageHeader,
  formatUptime,
  nodeStatePresentation,
} from "@/ui";
import { AgentConnectionSection } from "@/features/servers/ui/AgentConnectionSection";
import type { ServerDetailPageProps, ServerDcData } from "@/shared/api/types-pages/pages";
import { useIsDesktop } from "@/shared/hooks";

import { useRelativeTime } from "./useRelativeTime";
import { ServerActionsDropdown } from "./ServerActionsDropdown";
import { classifyMode } from "./classifyMode";
import { useFallbackEscalation } from "./useFallbackEscalation";
import { RelativeTimeBadge } from "./components/RelativeTimeBadge";
import { ServerHero } from "./components/ServerHero";
import { MobileLayout } from "./components/MobileLayout";
import { DesktopLayout } from "./components/DesktopLayout";
import { MeDownHero } from "./components/MeDownHero";
import { TelemtUnreachableBanner } from "./components/TelemtUnreachableBanner";
import { BadConnectionsCard } from "./components/BadConnectionsCard";
import { GatesPanel } from "./components/GatesPanel";
import { Fold } from "./components/Fold";
import { UpstreamsList } from "./components/UpstreamsList";
import { DcDetailSheet } from "./components/DcDetailSheet";
import { RenameDialog } from "./components/RenameDialog";
import { ChangeFleetGroupDialog } from "./components/ChangeFleetGroupDialog";
import { DeregisterDialog } from "./components/DeregisterDialog";
import { DirectRelayDesktop } from "./direct-relay/DirectRelayDesktop";
import { DirectRelayMobile } from "./direct-relay/DirectRelayMobile";
import type { PulseTickData } from "./components/PulseGrid";
import { buildPulseItems } from "./buildPulseItems";
import {
  computeAlertItems,
  computeBadRate,
  computeCoverageStats,
  statusSentence,
  toDcStripItems,
  toTimelineEvents,
} from "./format";
// P-04: lazy-load tab components so the initial ServerDetailContainer chunk
// stays tight. Each tab streams in only when it is rendered (active tab on
// mobile swipe / opened Fold on desktop). MePoolTab in particular is the
// heaviest sibling — extracting it shaves ~6-8 KB gzip off the page chunk.
const ConnectionsTab = lazy(() =>
  import("./tabs/ConnectionsTab").then((m) => ({ default: m.ConnectionsTab })),
);
const MePoolTab = lazy(() =>
  import("./tabs/MePoolTab").then((m) => ({ default: m.MePoolTab })),
);
const EventsTab = lazy(() =>
  import("./tabs/EventsTab").then((m) => ({ default: m.EventsTab })),
);
const ConfigTab = lazy(() =>
  import("./tabs/ConfigTab").then((m) => ({ default: m.ConfigTab })),
);

function TabSuspenseFallback() {
  const { t } = useTranslation("servers");
  return (
    <div className="px-4 py-6 text-xs text-fg-muted" aria-busy aria-live="polite">
      {t("loading.tab")}
    </div>
  );
}
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
  onChangeFleetGroup,
  fleetGroups,
  currentFleetGroupId,
  onDeregister,
  metricsChart,
  enrollmentHistorySlot,
  runtimeEventsSlot,
}: Readonly<ServerDetailPageProps>) {
  const { t } = useTranslation("servers");
  const { t: tc } = useTranslation("common");
  const { label: relativeTime, stale: relativeTimeStale } = useRelativeTime(lastUpdatedAt);
  // Render only the active breakpoint's layout instead of mounting both and
  // CSS-hiding one (which doubled render cost and mounted the hidden tree's
  // effects + lazy chunks). The two layouts consume the same derived data
  // below, so this is purely which tree mounts.
  const isDesktop = useIsDesktop();
  const { systemInfo, gates, connections, summary, dcs } = server;

  const [selectedDc, setSelectedDc] = useState<ServerDcData | null>(null);
  const [renameOpen, setRenameOpen] = useState(false);
  const [fleetGroupOpen, setFleetGroupOpen] = useState(false);
  const [deregisterOpen, setDeregisterOpen] = useState(false);

  // ─── Mode classification — must come first so downstream memos that
  // shape the hero / pulse strip / alerts can branch on it. Direct
  // nodes have no DC fleet so the DC-centric vocabulary the ME-era
  // helpers used would produce nonsense like
  // "STRAINED · 0 DC under coverage". See format.ts for the per-mode
  // wording.
  const mode = classifyMode({
    useMiddleProxy: server.useMiddleProxy,
    meRuntimeReady: server.meRuntimeReady,
    me2dcFallbackEnabled: server.me2dcFallbackEnabled,
  });

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
  const upstreamSummary = server.upstreamSummary;
  const directUpstreamHealthy =
    upstreamSummary?.healthyTotal ?? server.upstreams.filter((u) => u.healthy).length;
  const directUpstreamTotal =
    upstreamSummary?.configuredTotal ?? server.upstreams.length;
  const directFailRatePct5m = upstreamSummary?.failRatePct5m ?? 0;
  const directFailRateKnown = upstreamSummary?.failRateKnown ?? false;

  const pulseWord = useMemo(() => {
    if (mode === "direct") {
      return statusSentence(server.status, {
        mode: "direct",
        upstreamHealthy: directUpstreamHealthy,
        upstreamTotal: directUpstreamTotal,
        failRatePct5m: directFailRatePct5m,
        failRateKnown: directFailRateKnown,
      });
    }
    if (mode === "me_down") {
      return statusSentence(server.status, { mode: "me_down" });
    }
    return statusSentence(server.status, {
      mode,
      dcCount: sortedDcs.length,
      dcWarn,
      dcErr,
    });
  }, [
    mode,
    server.status,
    sortedDcs.length,
    dcWarn,
    dcErr,
    directUpstreamHealthy,
    directUpstreamTotal,
    directFailRatePct5m,
    directFailRateKnown,
  ]);
  const dcItems = useMemo(() => toDcStripItems(sortedDcs), [sortedDcs]);
  const alertItems = useMemo(
    () =>
      computeAlertItems({
        mode,
        sortedDcs,
        gates,
        hasInitState: Boolean(initState),
        upstreamSummary,
      }),
    [mode, sortedDcs, gates, initState, upstreamSummary],
  );
  const timelineEvents = useMemo(() => toTimelineEvents(server.events), [server.events]);

  // ─── Fallback escalation badge — wall-clock based via setTimeout so
  // the 30-min escalation flips even when the page sits idle without a
  // server re-fetch. See the hook's doc comment.

  const fallback = useFallbackEscalation(mode, server.fallbackEnteredAtUnix);

  // Mobile subtitle — same status sentence the desktop hero uses, plus
  // compact meta (version + uptime + optional config reload count).
  const subtitle = useMemo(
    () =>
      [
        pulseWord.toLowerCase(),
        t("detail.subtitleVersion", { version: systemInfo.version }),
        t("detail.subtitleUp", { value: formatUptime(systemInfo.uptimeSeconds) }),
        systemInfo.configReloadCount > 0
          ? t("detail.subtitleReloads", { count: systemInfo.configReloadCount })
          : null,
      ]
        .filter(Boolean)
        .join(" · "),
    [pulseWord, systemInfo.version, systemInfo.uptimeSeconds, systemInfo.configReloadCount, t],
  );

  // ─── Pulse ribbon ────────────────────────────────────────────────
  // The desktop (4-col) and mobile (2×2) ribbons share a single builder
  // (buildPulseItems) and differ only in hint verbosity. Pulling the
  // primitive fields out of the `connections`/`summary` objects first
  // lets the memos depend on value-level scalars — so a fresh
  // server-query object with identical numbers doesn't churn the cells,
  // and exhaustive-deps is satisfied without a disable.
  const { current, currentMe, currentDirect, activeUsers } = connections;
  const { connectionsTotal, configuredUsers, connectionsBadTotal } = summary;

  const desktopPulseItems = useMemo<PulseTickData[]>(
    () =>
      buildPulseItems(t, "desktop", {
        current,
        currentMe,
        currentDirect,
        activeUsers,
        connectionsTotal,
        configuredUsers,
        connectionsBadTotal,
        badRate,
        avgCoverage,
        minCoverage,
        dcOk,
        dcWarn,
        dcErr,
      }),
    [
      t,
      current,
      currentMe,
      currentDirect,
      activeUsers,
      connectionsTotal,
      configuredUsers,
      connectionsBadTotal,
      badRate,
      avgCoverage,
      minCoverage,
      dcOk,
      dcWarn,
      dcErr,
    ],
  );

  const mobilePulseItems = useMemo<PulseTickData[]>(
    () =>
      buildPulseItems(t, "mobile", {
        current,
        currentMe,
        currentDirect,
        activeUsers,
        connectionsTotal,
        configuredUsers,
        connectionsBadTotal,
        badRate,
        avgCoverage,
        minCoverage,
        dcOk,
        dcWarn,
        dcErr,
      }),
    [
      t,
      current,
      currentMe,
      currentDirect,
      activeUsers,
      connectionsTotal,
      configuredUsers,
      connectionsBadTotal,
      badRate,
      avgCoverage,
      minCoverage,
      dcOk,
      dcWarn,
      dcErr,
    ],
  );

  const connectionsContent = useMemo(
    () => (
      <Suspense fallback={<TabSuspenseFallback />}>
        <ConnectionsTab server={server} />
      </Suspense>
    ),
    [server],
  );
  const mePoolContent = useMemo(
    () => (
      <Suspense fallback={<TabSuspenseFallback />}>
        <MePoolTab server={server} />
      </Suspense>
    ),
    [server],
  );
  const eventsContent = useMemo(
    () => (
      <Suspense fallback={<TabSuspenseFallback />}>
        <EventsTab server={server} />
      </Suspense>
    ),
    [server],
  );
  const configContent = useMemo(
    () => (
      <Suspense fallback={<TabSuspenseFallback />}>
        <ConfigTab server={server} />
      </Suspense>
    ),
    [server],
  );

  // Mobile tab content for gates + upstreams — mirrors the desktop
  // "one card, two columns" composition but stacks vertically so it
  // reads well in a narrow swipe pane.
  const gatesUpstreamsContent = useMemo(
    () => (
      <div className="flex flex-col gap-4">
        <div className="flex flex-col gap-2">
          <span className="text-sm font-semibold text-fg">{t("detail.gates.title")}</span>
          {/* The mobile gates tab is only mounted inside the ME-mode
              MobileLayout; Direct/Fallback get a dedicated GatesPanel
              from the DirectRelay variants. */}
          <GatesPanel gates={gates} mode="me" />
        </div>
        <div className="flex flex-col gap-2">
          <div className="flex items-center justify-between">
            <span className="text-sm font-semibold text-fg">{t("detail.upstreams.title")}</span>
            <span className="text-nano font-mono text-fg-muted">
              {t("detail.upstreams.peers", { count: server.upstreams.length })}
            </span>
          </div>
          <UpstreamsList upstreams={server.upstreams} />
        </div>
      </div>
    ),
    [gates, server.upstreams, t],
  );

  // Diagnostics tab removed — version + reload counts now ride in the
  // PageHeader subtitle. Upstreams tab is folded into "Gates &
  // Upstreams" so the mobile flow mirrors the desktop card.
  const mobileTabs = useMemo(
    () => [
      { id: "connections", label: t("detail.folds.topClients"), content: connectionsContent },
      { id: "me-pool", label: t("detail.folds.mePool"), content: mePoolContent },
      { id: "gates", label: `${t("detail.gates.title")} & ${t("detail.upstreams.title")}`, content: gatesUpstreamsContent },
      { id: "events", label: t("detail.folds.events"), content: eventsContent },
      { id: "config", label: t("config.tab"), content: configContent },
    ],
    [connectionsContent, mePoolContent, gatesUpstreamsContent, eventsContent, configContent, t],
  );

  // Stable handlers so the dropdowns/sheets don't churn on every parent
  // re-render. The page's render cost is dominated by the data widgets
  // below, not by these callbacks.
  const handleSelectDc = useCallback((dc: Readonly<ServerDcData>) => setSelectedDc(dc), []);
  const handleCloseDc = useCallback(() => setSelectedDc(null), []);
  const handleOpenRename = useCallback(
    () => (onRename ? setRenameOpen(true) : undefined),
    [onRename],
  );
  const handleOpenChangeFleetGroup = useCallback(
    () => (onChangeFleetGroup ? setFleetGroupOpen(true) : undefined),
    [onChangeFleetGroup],
  );
  const handleOpenDeregister = useCallback(
    () => (onDeregister ? setDeregisterOpen(true) : undefined),
    [onDeregister],
  );
  const handleCloseDeregister = useCallback(() => setDeregisterOpen(false), []);

  return (
    <ServerDetailProvider server={server} serverId={server.id}>
      <div className="px-4 md:px-8 pt-3 pb-3">
        <Breadcrumbs items={[{ label: t("detail.breadcrumbServers"), onClick: onBack }, { label: server.name }]} />
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
              <NodeStateBadge state={server.state} label={tc(nodeStatePresentation(server.state).labelKey)} />
              <ServerActionsDropdown
                onReload={onReload}
                onBoostDetail={onBoostDetail}
                onRename={onRename ? handleOpenRename : undefined}
                onChangeFleetGroup={onChangeFleetGroup ? handleOpenChangeFleetGroup : undefined}
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
        onChangeFleetGroup={onChangeFleetGroup ? handleOpenChangeFleetGroup : undefined}
        onDeregister={onDeregister ? handleOpenDeregister : undefined}
      />

      <div className="px-4 md:px-8 flex flex-col gap-6 pb-8 pt-6">
        {server.telemtUnreachable ? (
          <TelemtUnreachableBanner sinceUnix={server.telemtUnreachableSinceUnix} />
        ) : (
          <>
            {mode === "me" &&
              (isDesktop ? (
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
              ) : (
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
              ))}

            {(mode === "direct" || mode === "fallback") &&
              (isDesktop ? (
                <DirectRelayDesktop
                  server={server}
                  initState={initState}
                  alertItems={alertItems}
                  metricsChart={metricsChart}
                  mode={mode}
                  fallback={fallback}
                />
              ) : (
                <DirectRelayMobile
                  server={server}
                  initState={initState}
                  metricsChart={metricsChart}
                  mode={mode}
                  fallback={fallback}
                />
              ))}

            {mode === "me_down" && <MeDownHero recentEvents={server.events} />}

            {/* Telemt 3.4.10 bad-connection + handshake-failure breakdown.
                Same data flow for every transport mode — the classifier
                runs on the inbound TCP path before the connection is
                routed, so ME, Direct and Fallback all accumulate these
                counters. me_down still gets the panel since the agent
                continues to scrape Telemt while the ME pool is down. */}
            {mode !== "me_down" && (
              <BadConnectionsCard
                connectionsBadByClass={server.summary.connectionsBadByClass}
                handshakeFailuresByClass={server.summary.handshakeFailuresByClass}
              />
            )}
          </>
        )}

        {/* Config tab — surfaced on desktop as a fold so it's reachable in
            every transport mode (ME, Direct/Fallback, ME-down), mirroring the
            mobile swipe tab above. Mobile gets it via mobileTabs; the fold is
            desktop-only to avoid a duplicate mount under the swipe pane. */}
        <div className="hidden md:block">
          <Fold title={t("config.tab")} defaultOpen={false}>
            {configContent}
          </Fold>
        </div>

        {agentConnection && (
          <AgentConnectionSection
            data={agentConnection}
            onAllowReEnrollment={onAllowReEnrollment ?? noop}
            onRevokeGrant={onRevokeGrant ?? noop}
          />
        )}

        {/*
          Phase-1 observability: the container passes an EnrollmentHistory
          slot here so this presentational page does not need a
          QueryClient in unit tests. The slot is rendered as-is below the
          AgentConnectionSection card.
        */}
        {enrollmentHistorySlot}
        {/*
          Phase-3 observability: parallel slot for the RuntimeEvents
          Fold. Same rationale as enrollmentHistorySlot — the wired
          component owns its own data dependencies (React Query +
          WebSocket), so we accept it as a node from the container.
        */}
        {runtimeEventsSlot}
      </div>

      {/* Shared DC detail sheet — opens from mobile strip, desktop radar, and desktop tiles. */}
      <DcDetailSheet selectedDc={selectedDc} onClose={handleCloseDc} />

      <RenameDialog
        open={renameOpen}
        onOpenChange={setRenameOpen}
        currentName={server.name}
        onRename={onRename}
      />

      <ChangeFleetGroupDialog
        open={fleetGroupOpen}
        onOpenChange={setFleetGroupOpen}
        currentFleetGroupId={currentFleetGroupId ?? ""}
        fleetGroups={fleetGroups ?? []}
        onChange={onChangeFleetGroup}
      />

      <DeregisterDialog
        open={deregisterOpen}
        onClose={handleCloseDeregister}
        onConfirm={onDeregister}
      />
    </ServerDetailProvider>
  );
}
