import React from "react";
import { CircleOff, Undo2 } from "lucide-react";
import { Link } from "@tanstack/react-router";
import { getTelemetryFieldHelp, type TelemetryHelpMode } from "../telemetry/help-metadata";

import type { ServerCardDetails as ServerCardDetailsModel } from "./dashboard-view-model";

type ServerCardDetailsProps = {
  details: ServerCardDetailsModel;
  serverId: string;
  lastSeenText: string;
  expanded?: boolean;
  helpMode?: TelemetryHelpMode;
};

export function ServerCardDetails({
  details,
  serverId,
  lastSeenText,
  expanded = true,
  helpMode = "basic",
}: ServerCardDetailsProps) {
  return (
    <div className="server-card-details" aria-hidden={!expanded}>
      <div className="server-card-details-head">
        <div className="server-card-details-title">
          {details.isOffline ? "Offline" : "DC Status"}
        </div>
        <div className="server-card-details-close">
          <Undo2 />
          Back
        </div>
      </div>

      {details.isOffline ? (
        <div className="server-card-offline">
          <CircleOff />
          <div className="server-card-offline-title">Server unavailable</div>
          <div className="server-card-offline-text">DC data is unavailable</div>
          <div className="server-card-offline-last-seen">{lastSeenText}</div>
        </div>
      ) : (
        <>
          <table className="server-card-table">
            <thead>
              <tr>
                <th />
                <th>DC</th>
                <th>RTT</th>
                <th>Writers</th>
                <th>Coverage</th>
              </tr>
            </thead>
            <tbody>
              {details.rows.map((row) => (
                <tr key={row.dc}>
                  <td>
                    <span className="server-card-row-status" data-tone={row.health} />
                  </td>
                  <td className="server-card-table-name">{row.dcText}</td>
                  <td className="server-card-rtt" data-tone={getRttTone(row.rttText)}>
                    {row.rttText}
                  </td>
                  <td>{row.writersText}</td>
                  <td className="server-card-coverage" data-tone={row.health}>
                    {row.coverageText}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {helpMode === "full" && getTelemetryFieldHelp("DC Health") ? (
            <div className="mt-3 text-[11px] text-text-3">{getTelemetryFieldHelp("DC Health")}</div>
          ) : null}
        </>
      )}

      <Link
        className="server-card-link"
        params={{ serverId }}
        tabIndex={expanded ? 0 : -1}
        to="/servers/$serverId"
      >
        Open server page
      </Link>
    </div>
  );
}

function getRttTone(rttText: string): "ok" | "warn" | "bad" {
  const numericValue = Number.parseInt(rttText.replace(/[^0-9]/g, ""), 10);

  if (!Number.isFinite(numericValue)) {
    return "bad";
  }

  if (numericValue >= 500) {
    return "bad";
  }

  if (numericValue >= 100) {
    return "warn";
  }

  return "ok";
}
