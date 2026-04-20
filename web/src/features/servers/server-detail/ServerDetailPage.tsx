import { useState } from "react";
import { cn } from "@/ui/lib/cn";
import { formatUptime } from "@/ui/lib/format";
import { coverageColor } from "@/ui/lib/status";
import { FieldLabel, MonoValue } from "@/ui/primitives";
import { InitCard } from "@/ui/primitives/InitCard";
import { StatusBeacon } from "@/ui/primitives/StatusBeacon";
import { Badge } from "@/ui/primitives/Badge";
import { PageHeader } from "@/ui/layout/PageHeader";
import { SectionHeader } from "@/ui/layout/SectionHeader";
import { Breadcrumbs } from "@/ui/compositions/Breadcrumbs";
import { GaugeStrip } from "@/ui/compositions/GaugeStrip";
import { DCScrollStrip } from "@/features/servers/ui/DCScrollStrip";
import { AlertStrip } from "@/ui/compositions/AlertStrip";
import { SwipeTabView } from "@/ui/compositions/SwipeTabView";
import { AgentConnectionSection } from "@/features/servers/ui/AgentConnectionSection";
import { MetricsChartSection } from "@/features/dashboard/ui/MetricsChartSection";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/ui/base/tabs";
import { Sheet, SheetContent, SheetHeader, SheetTitle, SheetBody } from "@/ui/base/sheet";
import type { ServerDetailPageProps, ServerDcData } from "@/shared/api/types-pages/pages";

import { useRelativeTime } from "./useRelativeTime";
import { ServerActionsDropdown } from "./ServerActionsDropdown";
import { DcTable } from "./DcTable";
import { ConnectionsTab } from "./tabs/ConnectionsTab";
import { MePoolTab } from "./tabs/MePoolTab";
import { UpstreamsTab } from "./tabs/UpstreamsTab";
import { DiagnosticsTab } from "./tabs/DiagnosticsTab";
import { EventsTab } from "./tabs/EventsTab";

const noop = () => {};

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

  // DC sort: problematic (low coverage) first
  const sortedDcs = [...dcs].sort((a, b) => a.coveragePct - b.coveragePct);

  // Mobile DC sheet state
  const [selectedDc, setSelectedDc] = useState<ServerDcData | null>(null);

  // Rename dialog state
  const [renameOpen, setRenameOpen] = useState(false);
  const [renameValue, setRenameValue] = useState(server.name);

  // Deregister confirmation state
  const [deregisterOpen, setDeregisterOpen] = useState(false);

  // KPI gauge items
  const minCoverage = sortedDcs.length > 0 ? Math.min(...sortedDcs.map((d) => d.coveragePct)) : 100;
  const badRate =
    summary.connectionsTotal > 0
      ? ((summary.connectionsBadTotal / summary.connectionsTotal) * 100).toFixed(1)
      : "0.0";

  const gaugeItems = [
    { value: connections.current.toLocaleString(), label: "Connections" },
    { value: connections.activeUsers.toLocaleString(), label: "Active Users" },
    { value: badRate, unit: "%", label: "Bad Rate" },
    { value: String(minCoverage), unit: "%", label: "Coverage" },
  ];

  // DC strip items (mobile)
  const dcItems = sortedDcs.map((dc) => ({
    code: `DC${dc.dc}`,
    city: `DC ${dc.dc}`,
    latency: dc.rttMs ?? 0,
    load: dc.load,
    status:
      dc.coveragePct < 70
        ? ("error" as const)
        : dc.coveragePct < 100
          ? ("warn" as const)
          : ("ok" as const),
  }));

  // Alerts
  const alertItems: { severity: "crit" | "warn"; message: string; source: string }[] = [];
  if (!initState) {
    sortedDcs
      .filter((dc) => dc.coveragePct < 100)
      .forEach((dc) => {
        alertItems.push({
          severity: dc.coveragePct < 70 ? ("crit" as const) : ("warn" as const),
          message: `DC${dc.dc} coverage at ${dc.coveragePct}% (${dc.aliveWriters}/${dc.requiredWriters} writers)`,
          source: "dc-coverage",
        });
      });
  }
  if (gates.degraded) {
    alertItems.unshift({
      severity: "crit" as const,
      message: "Server operating in degraded mode",
      source: "gates",
    });
  }

  const subtitle =
    `${systemInfo.version} · ${formatUptime(systemInfo.uptimeSeconds)} · ${server.ip ?? ""}`.replace(
      / · $/,
      "",
    );

  // Tab content
  const connectionsContent = <ConnectionsTab server={server} />;
  const mePoolContent = <MePoolTab server={server} />;
  const upstreamsContent = <UpstreamsTab server={server} />;
  const diagnosticsContent = <DiagnosticsTab server={server} />;
  const eventsContent = <EventsTab server={server} />;

  const mobileTabs = [
    { id: "connections", label: "Connections", content: connectionsContent },
    { id: "me-pool", label: "ME Pool", content: mePoolContent },
    { id: "upstreams", label: "Upstreams", content: upstreamsContent },
    { id: "diagnostics", label: "Diagnostics", content: diagnosticsContent },
    { id: "events", label: "Events", content: eventsContent },
  ];

  return (
    <>
      {/* Breadcrumbs */}
      <div className="px-4 md:px-8 pt-3">
        <Breadcrumbs items={[{ label: "Servers", onClick: onBack }, { label: server.name }]} />
      </div>

      {/* Page header */}
      <PageHeader
        title={server.name}
        subtitle={subtitle}
        trailing={
          <div className="flex items-center gap-3">
            {relativeTime && (
              <span
                className={cn(
                  "text-[10px] font-mono tabular-nums inline-flex items-center gap-1 rounded-full px-2 py-0.5 border transition-colors duration-500",
                  relativeTimeStale
                    ? "bg-status-warn/10 border-status-warn/15 text-status-warn"
                    : "bg-status-ok/10 border-status-ok/15 text-fg-muted",
                )}
              >
                <span className="text-[11px] animate-spin" style={{ animationDuration: "3s" }}>
                  ↻
                </span>
                {relativeTime}
              </span>
            )}
            <StatusBeacon status={server.status} size="xs" />
            <ServerActionsDropdown
              onReload={onReload}
              onBoostDetail={onBoostDetail}
              onRename={
                onRename
                  ? () => {
                      setRenameValue(server.name);
                      setRenameOpen(true);
                    }
                  : undefined
              }
              onDeregister={onDeregister ? () => setDeregisterOpen(true) : undefined}
            />
          </div>
        }
      />

      <div className="px-4 md:px-8 flex flex-col gap-6 pb-8">
        {/* Badges row */}
        <div className="flex flex-wrap gap-2">
          <Badge variant={gates.acceptingNewConnections ? "ok" : "error"}>
            {gates.acceptingNewConnections ? "Accepting" : "Blocked"}
          </Badge>
          <Badge variant={gates.meRuntimeReady ? "ok" : "warn"}>
            {gates.meRuntimeReady ? "ME Ready" : "ME Not Ready"}
          </Badge>
          <Badge variant={gates.useMiddleProxy ? "accent" : "default"}>
            {gates.useMiddleProxy ? "Middle Proxy" : "Direct"}
          </Badge>
          {gates.me2dcFallbackEnabled && <Badge variant="default">Fallback</Badge>}
          {gates.rerouteActive && <Badge variant="warn">Reroute Active</Badge>}
          {gates.readOnly && <Badge variant="warn">Read-Only</Badge>}
          {gates.degraded && <Badge variant="error">Degraded</Badge>}
        </div>

        {/* Init Card */}
        {initState && <InitCard {...initState} />}

        {/* KPI Strip */}
        <GaugeStrip items={gaugeItems} />

        {/* Alerts */}
        {alertItems.length > 0 && <AlertStrip alerts={alertItems} />}

        {/* Performance Charts */}
        {metricsChart && metricsChart.points.length > 0 && (
          <MetricsChartSection
            points={metricsChart.points}
            resolution={metricsChart.resolution}
            timeRange={metricsChart.timeRange}
            onTimeRangeChange={metricsChart.onTimeRangeChange}
          />
        )}

        {/* Mobile: DCScrollStrip + SwipeTabView */}
        <div className="md:hidden flex flex-col gap-4">
          <div>
            <SectionHeader title="Data Centers" badge={sortedDcs.length} />
            <DCScrollStrip
              items={dcItems}
              onSelect={(code) => {
                const dcNum = parseInt(code.replace("DC", ""), 10);
                const found = sortedDcs.find((d) => d.dc === dcNum) ?? null;
                setSelectedDc(found);
              }}
            />
          </div>
          <SwipeTabView tabs={mobileTabs} />
        </div>

        {/* Desktop: DC DataTable + Tabs */}
        <div className="hidden md:flex flex-col gap-6">
          <div>
            <SectionHeader title="Data Centers" badge={sortedDcs.length} />
            <DcTable dcs={sortedDcs} />
          </div>

          <Tabs defaultValue="connections">
            <TabsList>
              <TabsTrigger value="connections">Connections</TabsTrigger>
              <TabsTrigger value="me-pool">ME Pool</TabsTrigger>
              <TabsTrigger value="upstreams">Upstreams</TabsTrigger>
              <TabsTrigger value="diagnostics">Diagnostics</TabsTrigger>
              <TabsTrigger value="events">Events</TabsTrigger>
            </TabsList>
            <TabsContent value="connections">{connectionsContent}</TabsContent>
            <TabsContent value="me-pool">{mePoolContent}</TabsContent>
            <TabsContent value="upstreams">{upstreamsContent}</TabsContent>
            <TabsContent value="diagnostics">{diagnosticsContent}</TabsContent>
            <TabsContent value="events">{eventsContent}</TabsContent>
          </Tabs>
        </div>

        {agentConnection && (
          <AgentConnectionSection
            data={agentConnection}
            onAllowReEnrollment={onAllowReEnrollment ?? noop}
            onRevokeGrant={onRevokeGrant ?? noop}
          />
        )}
      </div>

      {/* Mobile DC detail sheet */}
      <Sheet
        open={selectedDc !== null}
        onOpenChange={(open) => {
          if (!open) setSelectedDc(null);
        }}
      >
        <SheetContent side="bottom">
          {selectedDc && (
            <>
              <SheetHeader>
                <SheetTitle>DC{selectedDc.dc} Details</SheetTitle>
              </SheetHeader>
              <SheetBody>
                <div className="flex flex-col gap-4">
                  <div className="grid grid-cols-2 gap-x-4 gap-y-1.5 text-sm">
                    <span className="text-fg-muted">Coverage</span>
                    <span
                      className={`font-mono font-semibold ${coverageColor(selectedDc.coveragePct)}`}
                    >
                      {selectedDc.coveragePct}%
                    </span>
                    <span className="text-fg-muted">Available</span>
                    <span
                      className={`font-mono ${selectedDc.availablePct < 100 ? "text-status-warn" : "text-fg"}`}
                    >
                      {selectedDc.availablePct}%
                    </span>
                    <span className="text-fg-muted">Writers</span>
                    <span className="font-mono text-fg">
                      {selectedDc.aliveWriters}/{selectedDc.requiredWriters} alive
                    </span>
                    <span className="text-fg-muted">RTT</span>
                    <span
                      className={`font-mono ${(selectedDc.rttMs ?? 0) > 300 ? "text-status-error" : (selectedDc.rttMs ?? 0) > 100 ? "text-status-warn" : "text-fg"}`}
                    >
                      {selectedDc.rttMs != null ? `${selectedDc.rttMs}ms` : "—"}
                    </span>
                    <span className="text-fg-muted">Load</span>
                    <span className="font-mono text-fg">{selectedDc.load}</span>
                    <span className="text-fg-muted">Floor</span>
                    <span className="font-mono text-fg">
                      {selectedDc.floorMin}..{selectedDc.floorTarget}..{selectedDc.floorMax}
                      {selectedDc.floorCapped && (
                        <span className="text-status-warn ml-1">⚠ capped</span>
                      )}
                    </span>
                  </div>

                  {selectedDc.endpointWriters.length > 0 && (
                    <div className="flex flex-col gap-2">
                      <FieldLabel>Endpoints & Writers</FieldLabel>
                      {selectedDc.endpointWriters.map((ew) => (
                        <div key={ew.endpoint} className="flex items-center gap-2 text-sm">
                          <MonoValue>{ew.endpoint}</MonoValue>
                          <span className="text-fg-muted">→</span>
                          <MonoValue>
                            {ew.activeWriters} active writer{ew.activeWriters !== 1 ? "s" : ""}
                          </MonoValue>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              </SheetBody>
            </>
          )}
        </SheetContent>
      </Sheet>

      {/* Rename Sheet */}
      <Sheet open={renameOpen} onOpenChange={setRenameOpen}>
        <SheetContent>
          <SheetHeader>
            <SheetTitle>Rename Server</SheetTitle>
          </SheetHeader>
          <SheetBody>
            <form
              onSubmit={(e) => {
                e.preventDefault();
                const trimmed = renameValue.trim();
                if (trimmed && trimmed !== server.name) {
                  onRename?.(trimmed);
                }
                setRenameOpen(false);
              }}
              className="flex flex-col gap-4"
            >
              <label className="flex flex-col gap-1.5">
                <span className="text-sm text-fg-muted">Server Name</span>
                <input
                  type="text"
                  value={renameValue}
                  onChange={(e) => setRenameValue(e.target.value)}
                  className="rounded-xs border border-border bg-bg px-3 py-2 text-sm text-fg focus:outline-none focus:ring-2 focus:ring-accent"
                  autoFocus
                />
              </label>
              <div className="flex justify-end gap-2">
                <button
                  type="button"
                  onClick={() => setRenameOpen(false)}
                  className="px-3 py-1.5 text-sm rounded-xs border border-border text-fg hover:bg-bg-card-hover transition-colors"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  disabled={!renameValue.trim() || renameValue.trim() === server.name}
                  className="px-3 py-1.5 text-sm rounded-xs bg-accent text-white hover:bg-accent/90 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                >
                  Save
                </button>
              </div>
            </form>
          </SheetBody>
        </SheetContent>
      </Sheet>

      {/* Deregister Confirmation */}
      {deregisterOpen && (
        <div className="fixed inset-0 z-50 flex items-center justify-center">
          <div className="absolute inset-0 bg-black/60" onClick={() => setDeregisterOpen(false)} />
          <div className="relative z-10 bg-bg-card border border-border rounded-lg shadow-xl p-6 max-w-sm w-full mx-4">
            <h3 className="text-base font-semibold text-fg mb-2">Deregister Server</h3>
            <p className="text-sm text-fg-muted mb-4">
              This will disconnect the agent, revoke its credentials, and remove all associated
              data. This action cannot be undone.
            </p>
            <div className="flex justify-end gap-2">
              <button
                onClick={() => setDeregisterOpen(false)}
                className="px-3 py-1.5 text-sm rounded-xs border border-border text-fg hover:bg-bg-card-hover transition-colors"
              >
                Cancel
              </button>
              <button
                onClick={() => {
                  onDeregister?.();
                  setDeregisterOpen(false);
                }}
                className="px-3 py-1.5 text-sm rounded-xs bg-status-error text-white hover:bg-status-error/90 transition-colors"
              >
                Deregister
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
