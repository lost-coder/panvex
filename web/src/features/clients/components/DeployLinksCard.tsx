// R-Q-08: Deployments + per-target connection-links card extracted from
// ClientDetailPage.tsx. The page hands over the deployments array and
// optional agent label resolver; the card owns the link-strip layout.

import { lazy, Suspense, useMemo, useState, type ReactNode } from "react";
import { RotateCcw, QrCode, Share2 } from "lucide-react";
import { useTranslation } from "react-i18next";

import type { ResetOutcome } from "@/features/clients/hooks/useResetQuota";
import {
  Badge,
  Button,
  CopyButton,
  ProgressBar,
  Sheet,
  SheetBody,
  SheetContent,
  SheetHeader,
  SheetTitle,
  Spinner,
  deployVariant,
  formatAge,
  formatBytes,
  formatDateTime,
  formatQuota,
  type ClientDeploymentData,
} from "@/ui";

const LazyQRCode = lazy(() => import("@/ui/compositions/internal/QRCode"));

// navigator.share is mobile-first and absent on most desktop browsers;
// compute support once so the Share affordance only appears where it works.
function canShare(): boolean {
  return typeof navigator !== "undefined" && typeof navigator.share === "function";
}

interface QuotaCellProps {
  quotaUsedBytes: number;
  quotaLastResetUnix: number;
  dataQuotaBytes: number;
  /**
   * Reset-quota Phase 2: hookup for the per-row affordance. When
   * provided, the cell renders a "Reset" button beside the label and
   * surfaces inline progress / failure messages driven by `state`.
   * Container resolves to undefined for callers without operator
   * permission so the cell hides the action entirely.
   */
  onReset?: (() => void) | undefined;
  state?: ResetOutcome | undefined;
  /** Optional inline-dismiss for failure messages. */
  onDismiss?: (() => void) | undefined;
  /**
   * Reset-quota Phase 3: panel-recorded last reset timestamp + drift
   * flag (true when the panel believes a reset landed but Telemt's
   * reported `quotaLastResetUnix` is still older). When drift is
   * true the cell surfaces a warning badge so the operator can
   * re-run the reset. Both default to 0 / false so older backend
   * responses render unchanged.
   */
  panelLastResetUnix?: number | undefined;
  quotaResetDrift?: boolean | undefined;
}

/**
 * Reset-quota Phase 1 read-only visibility cell + Phase 2 per-row
 * Reset button. Three base render modes:
 *
 *   - quota configured + history: progress bar + "Used: X / Y" label
 *     + relative "Last reset: Nd ago".
 *   - quota configured + never reset: same bar, "Never reset" subline.
 *   - no quota configured: collapses to "X used (no quota)" when there
 *     is any traffic, else "—" (the visually quieter option per brief).
 *
 * When `onReset` is supplied the cell adds a small icon-button to the
 * trailing edge of the bar/label line. While a reset is in flight the
 * button is replaced with an inline "Resetting…" spinner; once the
 * job's target reaches a terminal status the cell renders the matching
 * translated message (success toast comes from the container).
 */
function QuotaCell({
  quotaUsedBytes,
  quotaLastResetUnix,
  dataQuotaBytes,
  onReset,
  state,
  onDismiss,
  panelLastResetUnix = 0,
  quotaResetDrift = false,
}: Readonly<QuotaCellProps>) {
  const { t } = useTranslation("clients");
  const hasQuota = dataQuotaBytes > 0;
  const resetLabel =
    quotaLastResetUnix > 0
      ? t("detail.quota.lastReset", { when: formatAge(quotaLastResetUnix) })
      : t("detail.quota.neverReset");
  const panelLabel =
    panelLastResetUnix > 0
      ? t("detail.quota.panelLastReset", { when: formatAge(panelLastResetUnix) })
      : "";
  const driftBadge = quotaResetDrift ? (
    <Badge variant="warn">{t("detail.quota.driftWarning")}</Badge>
  ) : null;

  // Inline reset affordance (Phase 2). Pulled out so both the
  // quota-configured and no-quota branches can render the same control
  // — `dataQuotaBytes === 0` does NOT mean the operator can't reset:
  // Telemt still tracks per-user used_bytes regardless of whether the
  // panel has set a quota limit, and operators may want to zero the
  // counter for clarity even on unbounded clients.
  const resetControl = renderResetControl({ t, onReset, state, onDismiss });

  if (!hasQuota) {
    if (quotaUsedBytes <= 0 && !resetControl && !driftBadge) {
      // Visually quieter option: collapse to em-dash when neither
      // quota, used-bytes nor a drift signal have any signal.
      return <span className="text-micro font-mono text-fg-muted">—</span>;
    }
    return (
      <div className="flex flex-col gap-1">
        <div className="flex items-center gap-2">
          <span className="text-micro font-mono text-fg-muted">
            {quotaUsedBytes > 0
              ? t("detail.quota.usedNoQuota", { used: formatBytes(quotaUsedBytes) })
              : "—"}
          </span>
          {resetControl}
          {driftBadge}
        </div>
        {panelLabel && (
          <div className="text-nano font-mono text-fg-muted">{panelLabel}</div>
        )}
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-1 min-w-[160px]">
      <div className="flex items-center gap-2">
        <div className="flex-1 min-w-0">
          <ProgressBar
            value={Math.max(0, quotaUsedBytes)}
            max={Math.max(1, dataQuotaBytes)}
            showValue
            size="sm"
            variant="threshold"
          />
        </div>
        {resetControl}
        {driftBadge}
      </div>
      <div className="text-micro font-mono text-fg-muted tabular-nums">
        {t("detail.quota.usedOfQuota", {
          used: formatBytes(quotaUsedBytes),
          quota: formatQuota(dataQuotaBytes),
        })}
      </div>
      <div className="text-nano font-mono text-fg-muted">{resetLabel}</div>
      {panelLabel && (
        <div className="text-nano font-mono text-fg-muted">{panelLabel}</div>
      )}
    </div>
  );
}

interface ResetControlArgs {
  t: ReturnType<typeof useTranslation>["t"];
  onReset: (() => void) | undefined;
  state: ResetOutcome | undefined;
  onDismiss: (() => void) | undefined;
}

/**
 * Renders the trailing icon-button + inline state message for the
 * Phase-2 reset affordance. Returned as null when the row has no
 * `onReset` callback (viewer role, or container chose to hide it).
 *
 * Inline message routing:
 *   - pending      → small spinner + "Resetting…" text.
 *   - success      → renders nothing inline; the container surfaces a
 *                    toast and the parent's re-query updates the bar.
 *   - unsupported  → translated "Reset unavailable on this node…"
 *                    (Telemt < 3.4.6).
 *   - readonly     → translated "Telemt is in read-only mode…".
 *   - failed       → translated "Reset failed: {{error}}" with the
 *                    server-supplied result_text.
 *
 * Failure messages get a dismiss button so the row can return to its
 * idle state once the operator has read the message; the success path
 * doesn't need one because the cell automatically returns to idle on
 * the next render of the parent (state reset by the container).
 */
function renderResetControl({ t, onReset, state, onDismiss }: ResetControlArgs): ReactNode {
  if (!onReset) return null;
  if (state?.kind === "pending") {
    return (
      <span className="inline-flex items-center gap-1 text-nano font-mono text-fg-muted">
        <Spinner size="sm" />
        {t("detail.quota.resetting")}
      </span>
    );
  }
  // Failure / readonly / unsupported render the inline message inside
  // the body below; keep the trigger button visible so the operator
  // can retry once the underlying condition is fixed (or dismiss).
  const buttonNode = (
    <Button
      variant="ghost"
      size="sm"
      type="button"
      onClick={onReset}
      title={t("detail.quota.resetButton")}
      aria-label={t("detail.quota.resetButton")}
      className="h-6 w-6 p-0 shrink-0"
    >
      <RotateCcw className="h-3 w-3" aria-hidden="true" />
    </Button>
  );
  if (!state || state.kind === "success") {
    return buttonNode;
  }
  const message = (() => {
    if (state.kind === "unsupported") return t("detail.quota.resetUnsupported");
    if (state.kind === "readonly") return t("detail.quota.resetReadOnly");
    return t("detail.quota.resetFailed", { error: state.error });
  })();
  return (
    <span className="inline-flex items-center gap-1 text-nano font-mono text-status-error">
      {message}
      {onDismiss && (
        <button
          type="button"
          onClick={onDismiss}
          className="ml-1 px-1 text-fg-muted hover:text-fg"
          aria-label={t("detail.quota.dismissResetFailure")}
        >
          ×
        </button>
      )}
      {buttonNode}
    </span>
  );
}

export { QuotaCell };

interface LinksStripProps {
  links: { classic: string[]; secure: string[]; tls: string[] };
}

function LinksStrip({ links }: Readonly<LinksStripProps>) {
  const { t } = useTranslation("clients");
  // U-08: full-screen QR for the link the operator tapped. Handing out a
  // connection link by QR / share sheet is the #1 phone task — copy alone
  // forces a clipboard round-trip the operator can't complete in person.
  const [qrLink, setQrLink] = useState<string | null>(null);
  const shareable = useMemo(() => canShare(), []);
  const onShare = (link: string) => {
    void navigator.share({ title: t("deployments.links.shareTitle"), text: link }).catch(() => {
      // User dismissed the share sheet, or the target rejected it — non-fatal.
    });
  };

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
      <div className="mt-2 text-micro font-mono text-fg-muted">{t("deployments.links.none")}</div>
    );
  }
  return (
    <div className="mt-2 flex flex-col gap-1.5">
      {groups.flatMap((g) =>
        g.items.map((item, idx) => (
          <div key={`${g.key}-${idx}`} className="flex items-center gap-1.5 min-w-0">
            {/* Label every row (not just the first) so each link in a group
                is identifiable — U-08 / the review's "indistinguishable
                links" complaint. */}
            <span className="text-nano font-mono uppercase tracking-wider text-fg-muted shrink-0 w-[56px]">
              {g.label}
            </span>
            <span className="font-mono text-xs text-fg truncate min-w-0 flex-1">
              {item}
            </span>
            <button
              type="button"
              onClick={() => setQrLink(item)}
              aria-label={t("deployments.links.qr")}
              title={t("deployments.links.qr")}
              className="shrink-0 p-1.5 rounded-xs text-fg-muted hover:text-fg hover:bg-bg-hover transition-colors focus-visible:outline-2 focus-visible:outline-accent"
            >
              <QrCode size={15} aria-hidden="true" />
            </button>
            {shareable && (
              <button
                type="button"
                onClick={() => onShare(item)}
                aria-label={t("deployments.links.share")}
                title={t("deployments.links.share")}
                className="shrink-0 p-1.5 rounded-xs text-fg-muted hover:text-fg hover:bg-bg-hover transition-colors focus-visible:outline-2 focus-visible:outline-accent"
              >
                <Share2 size={15} aria-hidden="true" />
              </button>
            )}
            <CopyButton text={item} />
          </div>
        )),
      )}

      <Sheet open={qrLink !== null} onOpenChange={(open) => { if (!open) setQrLink(null); }}>
        <SheetContent side="bottom">
          <SheetHeader>
            <SheetTitle>{t("deployments.links.qrTitle")}</SheetTitle>
          </SheetHeader>
          <SheetBody>
            {qrLink && (
              <div className="flex flex-col items-center gap-4 py-2">
                <div className="rounded-lg bg-white p-4">
                  <Suspense fallback={<div className="size-[220px]" />}>
                    <LazyQRCode value={qrLink} size={220} level="M" />
                  </Suspense>
                </div>
                <div className="flex items-center gap-2 w-full min-w-0">
                  <span className="font-mono text-xs text-fg-muted truncate min-w-0 flex-1">{qrLink}</span>
                  <CopyButton text={qrLink} />
                </div>
              </div>
            )}
          </SheetBody>
        </SheetContent>
      </Sheet>
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
  /**
   * Phase-2 reset-quota affordance hookup. The container supplies a
   * per-agent callback (undefined for viewers) plus the per-agent
   * outcome map driven by `useResetQuota`. The card forwards both to
   * each `QuotaCell` so the cell can render the spinner / inline
   * message without further round-trips through the parent.
   */
  onResetQuota?: ((agentId: string) => void) | undefined;
  resetStates?: Record<string, ResetOutcome> | undefined;
  onDismissResetState?: ((agentId: string) => void) | undefined;
}

export function DeployLinksCard({
  deployments,
  secretPendingRedeploy,
  agentLabels,
  dataQuotaBytes = 0,
  onResetQuota,
  resetStates,
  onDismissResetState,
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
          <span className="text-micro font-mono text-fg-muted">
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
                  <span className="font-mono text-nano text-fg-muted truncate">
                    {d.agentId.slice(0, 8)}…
                  </span>
                )}
                <Badge variant={tone}>{d.status}</Badge>
                {d.desiredOperation && d.desiredOperation !== "none" && (
                  <Badge variant="accent">{d.desiredOperation}</Badge>
                )}
                <span className="ml-auto text-micro font-mono text-fg-muted tabular-nums">
                  {d.lastAppliedAtUnix > 0
                    ? formatDateTime(d.lastAppliedAtUnix * 1000)
                    : t("deployments.neverApplied")}
                </span>
              </div>
              {d.lastError && (
                <div className="mt-1 text-micro font-mono text-status-error break-words">
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
                <span className="text-nano font-mono uppercase tracking-wider text-fg-muted">
                  {t("detail.quota.cellHeader")}
                </span>
                <QuotaCell
                  quotaUsedBytes={d.quotaUsedBytes}
                  quotaLastResetUnix={d.quotaLastResetUnix}
                  dataQuotaBytes={dataQuotaBytes}
                  onReset={onResetQuota ? () => onResetQuota(d.agentId) : undefined}
                  state={resetStates?.[d.agentId]}
                  onDismiss={
                    onDismissResetState ? () => onDismissResetState(d.agentId) : undefined
                  }
                  panelLastResetUnix={d.panelLastResetUnix}
                  quotaResetDrift={d.quotaResetDrift}
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
