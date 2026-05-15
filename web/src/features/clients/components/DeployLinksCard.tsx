// R-Q-08: Deployments + per-target connection-links card extracted from
// ClientDetailPage.tsx. The page hands over the deployments array and
// optional agent label resolver; the card owns the link-strip layout.

import { useTranslation } from "react-i18next";

import {
  Badge,
  CopyButton,
  ProgressBar,
  deployVariant,
  formatAge,
  formatBytes,
  formatQuota,
  type ClientDeploymentData,
} from "@/ui";

interface QuotaCellProps {
  quotaUsedBytes: number;
  quotaLastResetUnix: number;
  dataQuotaBytes: number;
}

/**
 * Reset-quota Phase 1 read-only visibility cell. Three render modes:
 *
 *   - quota configured + history: progress bar + "Used: X / Y" label
 *     + relative "Last reset: Nd ago".
 *   - quota configured + never reset: same bar, "Never reset" subline.
 *   - no quota configured: collapses to "X used (no quota)" when there
 *     is any traffic, else "—" (the visually quieter option per brief).
 */
function QuotaCell({
  quotaUsedBytes,
  quotaLastResetUnix,
  dataQuotaBytes,
}: Readonly<QuotaCellProps>) {
  const { t } = useTranslation("clients");
  const hasQuota = dataQuotaBytes > 0;
  const resetLabel =
    quotaLastResetUnix > 0
      ? t("detail.quota.lastReset", { when: formatAge(quotaLastResetUnix) })
      : t("detail.quota.neverReset");

  if (!hasQuota) {
    if (quotaUsedBytes <= 0) {
      // Visually quieter option: collapse to em-dash when neither
      // quota nor used-bytes have any signal.
      return <span className="text-[11px] font-mono text-fg-muted">—</span>;
    }
    return (
      <div className="text-[11px] font-mono text-fg-muted">
        {t("detail.quota.usedNoQuota", { used: formatBytes(quotaUsedBytes) })}
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-1 min-w-[160px]">
      <ProgressBar
        value={Math.max(0, quotaUsedBytes)}
        max={Math.max(1, dataQuotaBytes)}
        showValue
        size="sm"
        variant="threshold"
      />
      <div className="text-[11px] font-mono text-fg-muted tabular-nums">
        {t("detail.quota.usedOfQuota", {
          used: formatBytes(quotaUsedBytes),
          quota: formatQuota(dataQuotaBytes),
        })}
      </div>
      <div className="text-[10px] font-mono text-fg-muted">{resetLabel}</div>
    </div>
  );
}

export { QuotaCell };

interface LinksStripProps {
  links: { classic: string[]; secure: string[]; tls: string[] };
}

function LinksStrip({ links }: Readonly<LinksStripProps>) {
  const { t } = useTranslation("clients");
  type LinkGroup = { key: "tls" | "secure" | "classic"; label: string; items: string[] };
  const groups: LinkGroup[] = (
    [
      { key: "tls", label: t("deployments.links.tls"), items: links.tls },
      { key: "secure", label: t("deployments.links.secure"), items: links.secure },
      { key: "classic", label: t("deployments.links.classic"), items: links.classic },
    ] satisfies LinkGroup[]
  ).filter((g) => g.items.length > 0);
  if (groups.length === 0) {
    return (
      <div className="mt-2 text-[11px] font-mono text-fg-muted">{t("deployments.links.none")}</div>
    );
  }
  return (
    <div className="mt-2 flex flex-col gap-1.5">
      {groups.flatMap((g) =>
        g.items.map((item, idx) => (
          <div key={`${g.key}-${idx}`} className="flex items-center gap-2 min-w-0">
            <span className="text-[10px] font-mono uppercase tracking-wider text-fg-muted shrink-0 w-[56px]">
              {idx === 0 ? g.label : ""}
            </span>
            <span className="font-mono text-xs text-fg truncate min-w-0 flex-1">
              {item}
            </span>
            <CopyButton text={item} />
          </div>
        )),
      )}
    </div>
  );
}

export interface DeployLinksCardProps {
  deployments: ClientDeploymentData[];
  secretPendingRedeploy?: boolean | undefined;
  agentLabels?: Record<string, string> | undefined;
  /**
   * Client-level configured quota — same value across every per-agent
   * row, so the card pulls it as a single prop instead of duplicating
   * it on each `ClientDeploymentData`. 0/absent means "no quota
   * configured" and the cell collapses to a quieter line.
   */
  dataQuotaBytes?: number | undefined;
}

export function DeployLinksCard({
  deployments,
  secretPendingRedeploy,
  agentLabels,
  dataQuotaBytes = 0,
}: Readonly<DeployLinksCardProps>) {
  const { t } = useTranslation("clients");
  if (deployments.length === 0) {
    return (
      <div className="rounded-xs bg-bg-card border border-divider p-4 text-sm text-fg-muted text-center">
        {t("deployments.noneYet")}
      </div>
    );
  }
  return (
    <section className="rounded-xs bg-bg-card border border-divider overflow-hidden">
      <header className="px-4 py-3 border-b border-divider flex items-center justify-between gap-2">
        <div className="flex items-baseline gap-2">
          <span className="text-sm font-semibold text-fg">{t("deployments.title")}</span>
          <span className="text-[11px] font-mono text-fg-muted">
            {t("deployments.nodeCount", { count: deployments.length })}
          </span>
        </div>
        {secretPendingRedeploy && <Badge variant="warn">{t("deployments.secretRotatedBadge")}</Badge>}
      </header>
      <div className="flex flex-col">
        {deployments.map((d) => {
          const tone = deployVariant(d.status);
          const label = agentLabels?.[d.agentId];
          return (
            <div key={d.agentId} className="px-4 py-3 border-b border-divider last:border-b-0">
              <div className="flex items-center gap-2 flex-wrap">
                <span className="font-mono text-sm text-fg truncate">
                  {label ?? d.agentId}
                </span>
                {label && (
                  <span className="font-mono text-[10px] text-fg-muted truncate">
                    {d.agentId.slice(0, 8)}…
                  </span>
                )}
                <Badge variant={tone}>{d.status}</Badge>
                {d.desiredOperation && d.desiredOperation !== "none" && (
                  <Badge variant="accent">{d.desiredOperation}</Badge>
                )}
                <span className="ml-auto text-[11px] font-mono text-fg-muted tabular-nums">
                  {d.lastAppliedAtUnix > 0
                    ? new Date(d.lastAppliedAtUnix * 1000).toLocaleString()
                    : t("deployments.neverApplied")}
                </span>
              </div>
              {d.lastError && (
                <div className="mt-1 text-[11px] font-mono text-status-error break-words">
                  {d.lastError}
                </div>
              )}
              {/*
                Reset-quota Phase 1: per-agent "Used / quota" cell. Sits
                above the connection links so the operator sees usage
                state without scrolling past the link strip. The cell
                handles its own three render modes (quota+history,
                quota+never-reset, no-quota); the parent only forwards
                the values verbatim.
              */}
              <div className="mt-2 flex flex-col gap-1">
                <span className="text-[10px] font-mono uppercase tracking-wider text-fg-muted">
                  {t("detail.quota.cellHeader")}
                </span>
                <QuotaCell
                  quotaUsedBytes={d.quotaUsedBytes}
                  quotaLastResetUnix={d.quotaLastResetUnix}
                  dataQuotaBytes={dataQuotaBytes}
                />
              </div>
              <LinksStrip links={d.links} />
            </div>
          );
        })}
      </div>
    </section>
  );
}
