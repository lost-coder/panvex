import { useState } from "react";
import { useTranslation } from "react-i18next";
import { NodeStateBadge, nodeStatePresentation, type NodeState } from "@/ui";
import { cn } from "@/ui/lib/cn";
import type { Status } from "@/ui/tokens/colors";

export interface NodeDcInfo {
  dc: number;
  status: Status;
  rttMs: number | null;
  coveragePct?: number;
  load?: number;
}

export interface NodeSummaryCardProps {
  name: string;
  status: Status;
  connections: number;
  trafficBytes: number;
  cpuPct: number;
  memPct?: number;
  dcs: NodeDcInfo[];
  /** Full node state — when set, renders a status badge instead of the beacon dot. */
  state?: NodeState | undefined;
  /** Already-localized reason line (shown under the name when set). */
  reason?: string | undefined;
  defaultExpanded?: boolean;
  autoExpandOnIssue?: boolean;
  onClick?: () => void;
  className?: string;
}

const beaconColor = {
  ok: "bg-status-ok shadow-[0_0_6px_rgba(52,211,153,0.4)]",
  warn: "bg-status-warn shadow-[0_0_6px_rgba(245,158,11,0.4)]",
  error: "bg-status-error shadow-[0_0_6px_rgba(239,68,68,0.4)] animate-led-blink",
} as const;

const dcLed = {
  ok: "bg-status-ok",
  warn: "bg-status-warn",
  error: "bg-status-error animate-led-blink",
} as const;

function formatTraffic(bytes: number): string {
  if (bytes < 1024 ** 2) return `${(bytes / 1024).toFixed(0)} KB`;
  if (bytes < 1024 ** 3) return `${(bytes / 1024 ** 2).toFixed(1)} MB`;
  if (bytes < 1024 ** 4) return `${(bytes / 1024 ** 3).toFixed(2)} GB`;
  return `${(bytes / 1024 ** 4).toFixed(2)} TB`;
}

function hasIssues(dcs: NodeDcInfo[]): boolean {
  return dcs.some((d) => d.status !== "ok");
}

function dcSummaryCounts(dcs: NodeDcInfo[]): { err: number; warn: number; cls: string } | null {
  const err = dcs.filter((d) => d.status === "error").length;
  const warn = dcs.filter((d) => d.status === "warn").length;
  if (err > 0) return { err, warn: 0, cls: "text-status-error" };
  if (warn > 0) return { err: 0, warn, cls: "text-status-warn" };
  return null;
}

function pctColor(v: number): string {
  if (v >= 90) return "text-status-error";
  if (v >= 70) return "text-status-warn";
  return "text-fg";
}

export function NodeSummaryCard({
  name,
  status,
  connections,
  trafficBytes,
  cpuPct,
  memPct,
  dcs,
  state,
  reason,
  defaultExpanded,
  autoExpandOnIssue = true,
  onClick,
  className,
}: Readonly<NodeSummaryCardProps>) {
  const { t } = useTranslation("servers");
  const { t: tc } = useTranslation("common");
  const shouldAutoExpand = autoExpandOnIssue && hasIssues(dcs);
  const [expanded, setExpanded] = useState(defaultExpanded ?? shouldAutoExpand);
  const issue = dcSummaryCounts(dcs);
  const issueText = (() => {
    if (!issue) return null;
    if (issue.err > 0) return t("list.card.dcDown", { count: issue.err });
    return t("list.card.dcDegraded", { count: issue.warn });
  })();

  return (
    <div
      className={cn(
        "rounded-xs border bg-bg-card flex flex-col transition-colors",
        status === "ok" ? "border-border" : "border-border-hi",
        className,
      )}
    >
      {/* Header — always visible */}
      <div
        role="button"
        tabIndex={0}
        onClick={() => onClick?.()}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            onClick?.();
          }
        }}
        className="w-full text-left px-4 py-3 flex flex-col gap-2 hover:bg-bg-hover/40 transition-colors rounded-xs cursor-pointer"
      >
        {/* Row 1: name + status + issue badge + expand toggle */}
        <div className="flex items-center gap-3 w-full">
          {state ? (
            <NodeStateBadge state={state} label={tc(nodeStatePresentation(state).labelKey)} />
          ) : (
            <span className={cn("h-2.5 w-2.5 rounded-full shrink-0", beaconColor[status])} />
          )}
          <span className="text-sm font-mono font-medium text-fg truncate">{name}</span>

          {issue && (
            <span className={cn("text-nano font-mono shrink-0 ml-auto", issue.cls)}>
              {issueText}
            </span>
          )}
          {!issue && <span className="ml-auto" />}

          {dcs.length > 0 && (
            <button
              type="button"
              aria-label={expanded ? t("list.card.collapse") : t("list.card.expand")}
              onClick={(e) => {
                e.stopPropagation();
                setExpanded((v) => !v);
              }}
              className={cn(
                "shrink-0 ml-1 p-1 -mr-1 rounded-md hover:bg-bg-hover/80 transition-all text-fg-muted hover:text-fg",
              )}
            >
              <svg
                width="14"
                height="14"
                viewBox="0 0 16 16"
                fill="none"
                className={cn("transition-transform duration-200", expanded && "rotate-180")}
              >
                <path
                  d="M4 6l4 4 4-4"
                  stroke="currentColor"
                  strokeWidth="2"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                />
              </svg>
            </button>
          )}
        </div>

        {reason && (
          <span className="text-xs text-fg-muted leading-snug truncate pl-[22px]">{reason}</span>
        )}

        {/* Row 2: inline metrics — hidden on mobile, visible on md+ */}
        <div className="hidden md:flex items-center gap-4 pl-[22px] text-xs font-mono">
          <span className="text-fg-muted">
            <span className="text-fg">{connections.toLocaleString()}</span> {t("list.card.connections")}
          </span>
          <span className="text-fg-muted">
            <span className="text-fg">{formatTraffic(trafficBytes)}</span>
          </span>
          <span className="text-fg-muted">
            {"cpu "}<span className={pctColor(cpuPct)}>{cpuPct}%</span>
          </span>
          {memPct !== undefined && (
            <span className="text-fg-muted">
              {"mem "}<span className={pctColor(memPct)}>{memPct}%</span>
            </span>
          )}
        </div>
      </div>

      {/* Expanded panel */}
      {expanded && (
        <div className="border-t border-border">
          {/* Metrics — shown on mobile (hidden on md+ because already in header) */}
          <div className="flex items-center gap-4 px-4 py-2.5 text-xs font-mono md:hidden">
            <span className="text-fg-muted">
              <span className="text-fg">{connections.toLocaleString()}</span> {t("list.card.connections")}
            </span>
            <span className="text-fg-muted">
              <span className="text-fg">{formatTraffic(trafficBytes)}</span>
            </span>
            <span className="text-fg-muted">
              {"cpu "}<span className={pctColor(cpuPct)}>{cpuPct}%</span>
            </span>
            {memPct !== undefined && (
              <span className="text-fg-muted">
                {"mem "}<span className={pctColor(memPct)}>{memPct}%</span>
              </span>
            )}
          </div>

          {/* DC grid */}
          <div className="px-3 pb-3 md:pt-2.5">
            <div className="grid grid-cols-3 sm:grid-cols-4 gap-1">
              {dcs.map((dc) => (
                <div
                  key={dc.dc}
                  className="flex items-center gap-1.5 rounded-[6px] px-2 py-[5px] bg-bg/60"
                >
                  <span className={cn("h-[7px] w-[7px] rounded-full shrink-0", dcLed[dc.status])} />
                  <span className="text-micro font-mono font-medium text-fg leading-none">
                    {dc.dc}
                  </span>
                  <span className="text-nano font-mono text-fg-muted ml-auto leading-none">
                    {dc.rttMs === null ? "—" : `${dc.rttMs}`}
                  </span>
                </div>
              ))}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
