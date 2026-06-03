import { memo } from "react";
import { useTranslation } from "react-i18next";
import {
  Badge,
  MiniChart,
  StatusDot,
  formatBytes,
  type DashboardNodeData,
  type DashboardOverviewData,
} from "@/ui";
import { loadTone } from "../format";

interface OverviewPanelProps {
  data: DashboardOverviewData;
  onNodeClick?: ((nodeId: string) => void) | undefined;
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
    <div className="flex items-center gap-1.5 text-nano font-mono leading-none">
      <span className="w-7 text-fg-muted shrink-0 uppercase tracking-wider">{label}</span>
      {hasSeries && series && <MiniChart data={series} width={56} height={18} color={tone.chart} />}
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
// L-19: memo'd so unrelated dashboard ticks (incoming runtime events,
// other rows mutating) do not force every fleet row to rerender. Each
// row only depends on its immutable `node` snapshot and a stable
// onClick callback from the parent.
const FleetRow = memo(function FleetRow({ node, onClick }: Readonly<{ node: DashboardNodeData; onClick?: () => void }>) {
  const { t } = useTranslation("dashboard");
  const isProblem = node.status !== "ok";
  const badgeVariant = node.status === "error" ? "error" : "warn";
  const badgeLabel = node.status === "error" ? t("fleet.statusDown") : t("fleet.statusDegraded");
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
        <span className="flex items-baseline gap-1 text-micro font-mono tabular-nums shrink-0 md:hidden">
          <span className="text-fg">{node.connections.toLocaleString()}</span>
          <span className="text-fg-muted opacity-60">{t("fleet.connections")}</span>
        </span>
        <span className="text-micro font-mono text-fg-muted tabular-nums shrink-0 md:hidden">
          {formatBytes(node.trafficBytes)}
        </span>
      </div>

      {/* Mobile line 2: CPU and MEM side-by-side, each gets equal width.
          Desktop keeps them as trailing columns next to the identity cell
          with explicit width buckets so every row aligns vertically. */}
      <div className="flex items-center justify-between md:justify-end gap-4 md:gap-4 pl-4 md:pl-0">
        {/* Desktop-only conn + traffic columns — mobile renders them in
            line 1 next to the name. */}
        <span className="hidden md:flex items-baseline gap-1 text-micro font-mono tabular-nums shrink-0 w-[92px] justify-end">
          <span className="text-fg">{node.connections.toLocaleString()}</span>
          <span className="text-fg-muted opacity-60">{t("fleet.connections")}</span>
        </span>
        <span className="hidden md:inline text-micro font-mono text-fg-muted tabular-nums shrink-0 w-[64px] text-right">
          {formatBytes(node.trafficBytes)}
        </span>
        <LoadCell label="CPU" value={node.cpuPct} series={node.cpuSeries} />
        <LoadCell label="MEM" value={node.memPct} series={node.memSeries} />
      </div>
    </button>
  );
});

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
  const { t } = useTranslation("dashboard");
  const trimmedHealthy = healthy.slice(0, maxHealthy);
  const hiddenCount = Math.max(0, healthy.length - trimmedHealthy.length);
  const totalShown = attention.length + trimmedHealthy.length;

  if (totalShown === 0) {
    return (
      <div className="py-12 text-center text-sm text-fg-muted">{t("fleet.empty")}</div>
    );
  }

  return (
    <div className="flex flex-col">
      {attention.length > 0 && (
        <>
          <div className="px-4 pt-3 pb-1 flex items-center justify-between border-b border-divider">
            <span className="text-nano font-mono uppercase tracking-wider text-status-error">
              {t("fleet.sectionAttention")}
            </span>
            <span className="text-nano font-mono text-fg-muted">
              {t("fleet.sectionCount", { count: attention.length })}
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
            <div className="px-4 pt-3 pb-1 text-nano font-mono uppercase tracking-wider text-fg-muted border-b border-divider">
              {t("fleet.sectionHealthy")}
            </div>
          )}
          {trimmedHealthy.map((n) => (
            <FleetRow key={n.id} node={n} onClick={() => onNodeClick?.(n.id)} />
          ))}
        </>
      )}
      {hiddenCount > 0 && (
        <div className="px-3 py-2 text-micro font-mono text-fg-muted text-center border-t border-border">
          {t("fleet.more", { count: hiddenCount })}
        </div>
      )}
    </div>
  );
}

export function FleetPanel({
  data,
  onNodeClick,
  onViewAll,
}: OverviewPanelProps & { onViewAll?: (() => void) | undefined }) {
  const { t } = useTranslation("dashboard");
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
          <h2 className="text-sm font-semibold text-fg">{t("fleet.title")}</h2>
          <span className="text-micro font-mono text-fg-muted truncate">
            {t("fleet.summary", { count: totalFleet })}
            {issues > 0 && t("fleet.issues", { count: issues })}
          </span>
        </div>
        {onViewAll && (
          <button
            type="button"
            onClick={onViewAll}
            className="text-micro font-mono text-fg-muted hover:text-fg transition-colors shrink-0"
          >
            {t("fleet.viewAll")}
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
