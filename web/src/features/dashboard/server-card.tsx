import { useState } from "react";
import type { TelemetryServerSummary } from "@/lib/api";
import type { TelemetryHelpMode } from "../telemetry/help-metadata";
import {
  buildServerCardDetails,
  buildServerCardSummary,
} from "./dashboard-view-model";
import { ServerCardDetails } from "./server-card-details";
import { ServerCardSummary } from "./server-card-summary";

import "./server-card.css";

export function ServerCard({ item, helpMode = "basic" }: { item: TelemetryServerSummary; helpMode?: TelemetryHelpMode }) {
  const [expanded, setExpanded] = useState(false);
  const summary = buildServerCardSummary(item);
  const details = buildServerCardDetails(item);
  const lastContactAgeText = formatLastContactAge(item.agent.last_seen_at);
  const lastSeenText = `Last contact: ${lastContactAgeText}`;
  const hintText = details.isOffline
    ? `Server unavailable - last contact ${lastContactAgeText}`
    : `${item.reason} - press for DC details`;

  return (
    <article className="server-card-shell" data-expanded={expanded}>
      <div className="server-card-frame">
        <button
          aria-expanded={expanded}
          className="server-card-trigger"
          onClick={() => setExpanded((currentValue) => !currentValue)}
          type="button"
        >
          <ServerCardSummary
            expanded={expanded}
            helpMode={helpMode}
            hintText={hintText}
            summary={summary}
          />
        </button>
        <ServerCardDetails
          details={details}
          expanded={expanded}
          helpMode={helpMode}
          lastSeenText={lastSeenText}
          serverId={item.agent.id}
        />
      </div>
    </article>
  );
}

function formatLastContactAge(lastSeenAt: string): string {
  const lastSeenTimestamp = Date.parse(lastSeenAt);

  if (!Number.isFinite(lastSeenTimestamp)) {
    return "unknown";
  }

  const diffMs = Math.max(0, Date.now() - lastSeenTimestamp);
  const diffMinutes = Math.round(diffMs / 60_000);

  if (diffMinutes < 1) {
    return "just now";
  }

  if (diffMinutes < 60) {
    return `${diffMinutes} min ago`;
  }

  const diffHours = Math.round(diffMinutes / 60);
  if (diffHours < 24) {
    return `${diffHours} hr ago`;
  }

  const diffDays = Math.round(diffHours / 24);
  return `${diffDays} d ago`;
}
