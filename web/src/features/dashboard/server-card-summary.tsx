import React from "react";
import { ArrowRight } from "lucide-react";
import { getTelemetryFieldHelp, type TelemetryHelpMode } from "../telemetry/help-metadata";

import type { ServerCardSummary as ServerCardSummaryModel } from "./dashboard-view-model";

type ServerCardSummaryProps = {
  summary: ServerCardSummaryModel;
  expanded: boolean;
  hintText: string;
  helpMode?: TelemetryHelpMode;
};

type DcCountItem = {
  key: string;
  label: string;
  tone: "good" | "warn" | "bad";
  value: number;
};

export function ServerCardSummary({
  summary,
  expanded,
  hintText,
  helpMode = "basic",
}: ServerCardSummaryProps) {
  const countItems = buildDcCountItems(summary);
  const showExpandIcon = summary.statusText !== "Offline";

  return (
    <div className="server-card-summary">
      <div className="server-card-head">
        <div>
          <div className="server-card-name">{summary.nameText}</div>
          <div className="server-card-location">{summary.locationText}</div>
        </div>
        <span className="server-card-status" data-tone={summary.statusTone}>
          <span className="server-card-status-dot" />
          {summary.statusText}
        </span>
      </div>

      <div className="server-card-metrics">
        {summary.metrics.map((metric) => (
          <div key={metric.label} className="server-card-metric">
            <div className="server-card-metric-value">{metric.value}</div>
            <div className="server-card-metric-label">{metric.label}</div>
            {helpMode === "full" && getTelemetryFieldHelp(metric.label) ? (
              <div className="mt-1 text-[10px] leading-4 text-text-3">
                {getTelemetryFieldHelp(metric.label)}
              </div>
            ) : null}
          </div>
        ))}
      </div>

      <div className="server-card-summary-dc">
        <div className="server-card-dc-counts">
          {countItems.map((item) => (
            <div key={item.key} className="server-card-dc-count" data-tone={item.tone}>
              {item.value}
              <span className="server-card-dc-count-label">{item.label}</span>
            </div>
          ))}
        </div>
        <div className="server-card-dc-tags">
          {summary.dcTags.map((tag, index) => (
            <span key={`${summary.id}-${tag}-${index}`} className="server-card-dc-tag" data-tone={tag} />
          ))}
        </div>
      </div>

      <div className="server-card-expand-hint" aria-hidden={expanded}>
        {showExpandIcon ? <ArrowRight /> : null}
        <span>{hintText}</span>
      </div>
    </div>
  );
}

function buildDcCountItems(summary: ServerCardSummaryModel): DcCountItem[] {
  const downLabel =
    summary.statusText === "Offline" && summary.dcCounts.ok === 0 && summary.dcCounts.partial === 0
      ? "unreachable"
      : "critical";

  const items: DcCountItem[] = [
    { key: "ok", label: "OK", tone: "good", value: summary.dcCounts.ok },
    { key: "partial", label: "degraded", tone: "warn", value: summary.dcCounts.partial },
    { key: "down", label: downLabel, tone: "bad", value: summary.dcCounts.down },
  ];

  return items.filter((item) => item.value > 0);
}
