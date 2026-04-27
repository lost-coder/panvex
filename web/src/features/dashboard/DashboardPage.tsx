// Phase-7 redesign: handoff-style fleet overview. KPIs render as 4 dense
// tiles, the server list unifies attention + healthy nodes with a visual
// divider, and the header carries a live-refresh indicator.
import { ClientFormSheet } from "@/features/clients/ClientFormSheet";
import { DiscoveredClientsBanner } from "@/features/clients/DiscoveredClientsBanner";
import { useState } from "react";
import {
  Badge,
  Button,
  MiniChart,
  PageHeader,
  SectionHeader,
  Sheet,
  SheetBody,
  SheetContent,
  StatusDot,
  SwipeTabView,
  Timeline,
  formatBytes,
  type ClientFormData,
  type DashboardNodeData,
  type DashboardOverviewData,
  type DashboardPageProps,
  type DashboardTimelineData,
  type KpiItem,
} from "@/ui";

interface OverviewPanelProps {
  data: DashboardOverviewData;
  onNodeClick?: ((nodeId: string) => void) | undefined;
}

const TONE_VALUE_CLASS: Record<NonNullable<KpiItem["tone"]>, string> = {
  default: "text-fg",
  ok: "text-status-ok",
  warn: "text-status-warn",
  error: "text-status-error",
};

const SPARKLINE_COLOR_BY_TONE: Record<NonNullable<KpiItem["tone"]>, string> = {
  default: "var(--color-accent)",
  ok: "var(--color-status-ok)",
  warn: "var(--color-status-warn)",
  error: "var(--color-status-error)",
};

function KpiStrip({ kpis }: Readonly<{ kpis: DashboardOverviewData["kpis"] }>) {
  return (
    <>
      {/* Mobile: compact text — keeps the 4 KPIs in one line of signal */}
      <div className="flex flex-wrap items-center gap-x-5 gap-y-1 text-xs font-mono md:hidden">
        {kpis.map((k) => {
          const valueClass = k.tone ? TONE_VALUE_CLASS[k.tone] : "text-fg";
          return (
            <span key={k.label} className="text-fg-muted">
              {k.label.toLowerCase()}{" "}
              <span className={`font-medium ${valueClass}`}>{k.value}</span>
            </span>
          );
        })}
      </div>
      {/* Desktop: dense tiles — value + sparkline (if provided), delta + sub underneath */}
      <div className="hidden md:grid grid-cols-4 gap-3">
        {kpis.map((k) => {
          const tone: NonNullable<KpiItem["tone"]> = k.tone ?? "default";
          return (
            <div
              key={k.label}
              className="rounded-xs bg-bg-card border border-border px-4 py-3 flex flex-col gap-1 min-h-[88px]"
            >
              <div className="flex items-start justify-between gap-3">
                <div className="flex flex-col gap-0.5 min-w-0">
                  <span className="text-[10px] text-fg-muted uppercase tracking-wider">
                    {k.label}
                  </span>
                  <span
                    className={`text-2xl font-mono font-semibold leading-none tracking-tight ${TONE_VALUE_CLASS[tone]}`}
                  >
                    {k.value}
                  </span>
                </div>
                {k.series && k.series.length > 1 && (
                  <MiniChart
                    data={k.series}
                    width={90}
                    height={36}
                    color={SPARKLINE_COLOR_BY_TONE[tone]}
                  />
                )}
              </div>
              <div className="flex items-center gap-2 text-[10px] font-mono text-fg-muted mt-auto">
                {k.deltaLabel && (
                  <span
                    className={
                      k.deltaDirection === "up"
                        ? "text-status-ok"
                        : k.deltaDirection === "down"
                          ? "text-status-error"
                          : "text-fg-muted"
                    }
                  >
                    {k.deltaDirection === "up"
                      ? "▲"
                      : k.deltaDirection === "down"
                        ? "▼"
                        : "·"}{" "}
                    {k.deltaLabel}
                  </span>
                )}
                {k.sub && <span>{k.sub}</span>}
              </div>
            </div>
          );
        })}
      </div>
    </>
  );
}

// Removed ProblemRow — FleetRow now unifies healthy and attention nodes.

function loadTone(value: number): { chart: string; text: string } {
  if (value >= 90) return { chart: "var(--color-status-error)", text: "text-status-error" };
  if (value >= 70) return { chart: "var(--color-status-warn)", text: "text-status-warn" };
  return { chart: "var(--color-accent)", text: "text-fg" };
}

/**
 * Compact `CPU  ╭╮╱ 42%` cell. Renders a sparkline when series data is
 * available, otherwise falls back to the value-only text so newly
 * enrolled nodes (no history yet) don't leave a blank column.
 */
function LoadCell({
  label,
  value,
  series,
}: Readonly<{
  label: string;
  value: number;
  series: number[] | undefined;
}>) {
  const tone = loadTone(value);
  const hasSeries = Array.isArray(series) && series.length > 1;
  return (
    <div className="flex items-center gap-1.5 text-[10px] font-mono leading-none">
      <span className="w-7 text-fg-muted shrink-0 uppercase tracking-wider">{label}</span>
      {hasSeries && <MiniChart data={series!} width={56} height={18} color={tone.chart} />}
      <span className={`w-9 text-right tabular-nums shrink-0 ${tone.text}`}>{value}%</span>
    </div>
  );
}

/**
 * Unified fleet row for both healthy and attention nodes. Problem rows
 * share the same column shape (name on the left, CPU/MEM on the right)
 * so the operator's eye stays on a consistent axis — only the left
 * border tint, the subtle row background and the status badge call out
 * the degraded state.
 *
 * Mobile layout stacks into two lines: identity + badge above, metrics
 * below. Desktop keeps everything on a single row.
 */
function FleetRow({ node, onClick }: Readonly<{ node: DashboardNodeData; onClick?: () => void }>) {
  const isProblem = node.status !== "ok";
  const badgeVariant = node.status === "error" ? "error" : "warn";
  const badgeLabel = node.status === "error" ? "DOWN" : "DEGRADED";
  // `border-l-2` + `border-l-status-error` / `border-l-transparent` keep
  // the left-edge tint scoped; the separate `border-b border-divider`
  // rule on the button below then paints the row divider without being
  // clobbered by a full-row `border-transparent` shorthand.
  const rowClasses = isProblem
    ? "border-l-2 border-l-status-error bg-status-error/5 hover:bg-status-error/10"
    : "border-l-2 border-l-transparent hover:bg-bg-hover";
  return (
    <button
      type="button"
      onClick={onClick}
      // `border-divider` is the dedicated list-separator token (~14% on
      // dark, 18% on light). Strong enough to read as a real divider,
      // tokenized so both themes hit the same contrast target.
      className={`w-full flex flex-col gap-2 md:flex-row md:items-center md:gap-4 px-4 py-3 text-left transition-colors border-b border-divider last:border-b-0 min-h-[56px] ${rowClasses}`}
    >
      {/* Mobile line 1: StatusDot + name + badge? push conn + traffic to the
          far right so long names truncate cleanly without pushing numbers
          off-screen. Desktop collapses this row into a normal flex cell. */}
      <div className="flex items-center gap-2 min-w-0 md:flex-1">
        <StatusDot status={node.status} />
        <span className="text-sm font-mono text-fg font-medium truncate min-w-0 flex-1">
          {node.name}
        </span>
        {isProblem && <Badge variant={badgeVariant}>{badgeLabel}</Badge>}
        <span className="flex items-baseline gap-1 text-[11px] font-mono tabular-nums shrink-0 md:hidden">
          <span className="text-fg">{node.connections.toLocaleString()}</span>
          <span className="text-fg-muted opacity-60">conn</span>
        </span>
        <span className="text-[11px] font-mono text-fg-muted tabular-nums shrink-0 md:hidden">
          {formatBytes(node.trafficBytes)}
        </span>
      </div>

      {/* Mobile line 2: CPU and MEM side-by-side, each gets equal width.
          Desktop keeps them as trailing columns next to the identity cell
          with explicit width buckets so every row aligns vertically. */}
      <div className="flex items-center justify-between md:justify-end gap-4 md:gap-4 pl-4 md:pl-0">
        {/* Desktop-only conn + traffic columns — mobile renders them in
            line 1 next to the name. */}
        <span className="hidden md:flex items-baseline gap-1 text-[11px] font-mono tabular-nums shrink-0 w-[92px] justify-end">
          <span className="text-fg">{node.connections.toLocaleString()}</span>
          <span className="text-fg-muted opacity-60">conn</span>
        </span>
        <span className="hidden md:inline text-[11px] font-mono text-fg-muted tabular-nums shrink-0 w-[64px] text-right">
          {formatBytes(node.trafficBytes)}
        </span>
        <LoadCell label="CPU" value={node.cpuPct} series={node.cpuSeries} />
        <LoadCell label="MEM" value={node.memPct} series={node.memSeries} />
      </div>
    </button>
  );
}

function FleetList({
  attention,
  healthy,
  onNodeClick,
  maxHealthy = 12,
}: Readonly<{
  attention: DashboardNodeData[];
  healthy: DashboardNodeData[];
  onNodeClick?: ((id: string) => void) | undefined;
  maxHealthy?: number | undefined;
}>) {
  const trimmedHealthy = healthy.slice(0, maxHealthy);
  const hiddenCount = Math.max(0, healthy.length - trimmedHealthy.length);
  const totalShown = attention.length + trimmedHealthy.length;

  if (totalShown === 0) {
    return (
      <div className="py-12 text-center text-sm text-fg-muted">No servers registered yet.</div>
    );
  }

  return (
    <div className="flex flex-col">
      {attention.length > 0 && (
        <>
          <div className="px-4 pt-3 pb-1 flex items-center justify-between border-b border-divider">
            <span className="text-[10px] font-mono uppercase tracking-wider text-status-error">
              Needs attention
            </span>
            <span className="text-[10px] font-mono text-fg-muted">
              {attention.length} server{attention.length === 1 ? "" : "s"}
            </span>
          </div>
          {attention.map((n) => (
            <FleetRow key={n.id} node={n} onClick={() => onNodeClick?.(n.id)} />
          ))}
        </>
      )}
      {trimmedHealthy.length > 0 && (
        <>
          {attention.length > 0 && (
            <div className="px-4 pt-3 pb-1 text-[10px] font-mono uppercase tracking-wider text-fg-muted border-b border-divider">
              Healthy
            </div>
          )}
          {trimmedHealthy.map((n) => (
            <FleetRow key={n.id} node={n} onClick={() => onNodeClick?.(n.id)} />
          ))}
        </>
      )}
      {hiddenCount > 0 && (
        <div className="px-3 py-2 text-[11px] font-mono text-fg-muted text-center border-t border-border">
          + {hiddenCount} more — open the Servers page to see the full fleet
        </div>
      )}
    </div>
  );
}

function FleetPanel({
  data,
  onNodeClick,
  onViewAll,
}: OverviewPanelProps & { onViewAll?: (() => void) | undefined }) {
  const totalFleet = data.attentionNodes.length + data.healthyNodes.length;
  const issues = data.attentionNodes.length;
  return (
    <section className="rounded-xs bg-bg-card border border-border overflow-hidden">
      {/* Handoff-style card header: bold section title on the left, a
          muted servers/issues summary right next to it, and a ghost
          "View all →" link pushing to the right. Keeps the Fleet card
          self-titled instead of relying on the external SectionHeader. */}
      <header className="flex items-center justify-between gap-3 px-4 py-3 border-b border-divider">
        <div className="flex items-baseline gap-3 min-w-0">
          <h2 className="text-sm font-semibold text-fg">Fleet</h2>
          <span className="text-[11px] font-mono text-fg-muted truncate">
            {totalFleet} server{totalFleet === 1 ? "" : "s"}
            {issues > 0 && ` · ${issues} issue${issues === 1 ? "" : "s"}`}
          </span>
        </div>
        {onViewAll && (
          <button
            type="button"
            onClick={onViewAll}
            className="text-[11px] font-mono text-fg-muted hover:text-fg transition-colors shrink-0"
          >
            View all →
          </button>
        )}
      </header>
      <FleetList
        attention={data.attentionNodes}
        healthy={data.healthyNodes}
        onNodeClick={onNodeClick}
      />
    </section>
  );
}

function TimelinePanel({ data }: Readonly<{ data: DashboardTimelineData }>) {
  if (!data?.events) return null;
  return (
    <div className="flex flex-col gap-2 bg-bg-card border border-border rounded-xs p-4">
      <SectionHeader title="Recent Events" trailing={<Badge variant="accent">live</Badge>} />
      <Timeline
        events={data.events.slice(0, 8).map((e) => ({
          status: e.status === "info" ? ("ok" as const) : e.status,
          time: e.time,
          message: e.message,
          source: e.source,
        }))}
      />
    </div>
  );
}

const emptyFormData: ClientFormData = {
  name: "",
  userAdTag: "",
  userAdTagAuto: true,
  expirationRfc3339: "",
  maxTcpConns: 0,
  maxUniqueIps: 0,
  dataQuotaBytes: 0,
  fleetGroupIds: [],
  agentIds: [],
};

export function DashboardPage({
  overview,
  timeline,
  onNodeClick,
  onCreate,
  createLoading,
  createError,
  pendingDiscoveredCount,
  onDiscoveredClick,
  onViewAllServers,
}: Readonly<DashboardPageProps>) {
  const [createOpen, setCreateOpen] = useState(false);
  const [createData, setCreateData] = useState<ClientFormData>({ ...emptyFormData });

  return (
    <>
      <PageHeader
        title="Dashboard"
        subtitle="Realtime fleet overview · MTProto proxy operations"
        trailing={
          <div className="flex items-center gap-3">
            {/* Phase-7 live indicator: mirrors the 15s refetch interval of
                useDashboardData so the operator can see that the page is
                pulling fresh telemetry. */}
            <span
              aria-live="polite"
              className="hidden sm:flex items-center gap-1.5 text-[11px] font-mono text-fg-muted"
            >
              <StatusDot status="ok" className="animate-pulse" />
              live · 15s refresh
            </span>
            {onCreate && (
              <Button
                size="sm"
                onClick={() => {
                  setCreateData({ ...emptyFormData });
                  setCreateOpen(true);
                }}
              >
                Add Client
              </Button>
            )}
          </div>
        }
      />
      <div className="px-4 md:px-8 pb-8 flex flex-col gap-5">
        {/* Phase-7 layout: banner + KPI tiles span full width. The Active
            Alerts block was removed — FleetList already surfaces problem
            nodes in a "Needs attention" section with the same severity
            signal, so a separate alerts card would be pure duplication. */}
        {!!pendingDiscoveredCount && (
          <DiscoveredClientsBanner count={pendingDiscoveredCount} onClick={onDiscoveredClick} />
        )}
        <KpiStrip kpis={overview.kpis} />

        {/* Mobile: swipe tabs between fleet and activity to avoid a long scroll. */}
        <div className="md:hidden">
          <SwipeTabView
            tabs={[
              {
                id: "fleet",
                label: "Fleet",
                content: (
                  <div className="pt-4">
                    <FleetPanel
                      data={overview}
                      onNodeClick={onNodeClick}
                      onViewAll={onViewAllServers}
                    />
                  </div>
                ),
              },
              {
                id: "timeline",
                label: "Activity",
                content: (
                  <div className="pt-4">
                    <TimelinePanel data={timeline} />
                  </div>
                ),
              },
            ]}
          />
        </div>

        {/* Desktop: fleet column gets ~2.2x the width of the activity column so
            the per-node rows have room for CPU/MEM load bars + traffic. */}
        <div className="hidden md:grid md:grid-cols-[minmax(0,2.2fr)_minmax(280px,1fr)] gap-6 items-start">
          {/* items-start prevents the grid from stretching the Fleet card
              to match a tall Recent Events column. */}
          <FleetPanel
            data={overview}
            onNodeClick={onNodeClick}
            onViewAll={onViewAllServers}
          />
          <TimelinePanel data={timeline} />
        </div>
      </div>

      {onCreate && (
        <Sheet
          open={createOpen}
          onOpenChange={(open) => {
            if (!open) setCreateOpen(false);
          }}
        >
          <SheetContent side="bottom" title="Add client">
            <SheetBody>
              <ClientFormSheet
                mode="create"
                data={createData}
                onChange={setCreateData}
                onSubmit={async () => {
                  await onCreate(createData);
                  if (!createError) setCreateOpen(false);
                }}
                onCancel={() => setCreateOpen(false)}
                loading={createLoading}
                error={createError}
              />
            </SheetBody>
          </SheetContent>
        </Sheet>
      )}
    </>
  );
}
