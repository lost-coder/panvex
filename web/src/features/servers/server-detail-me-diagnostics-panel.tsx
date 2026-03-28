import { getTelemetryFieldHelp, type TelemetryHelpMode } from "@/features/telemetry/help-metadata";

export function ServerDetailMeDiagnosticsPanel({
  stateText,
  helpMode,
  meRows,
  routingRows,
}: {
  stateText: string;
  helpMode: TelemetryHelpMode;
  meRows: Array<{ label: string; valueText: string }>;
  routingRows: Array<{ label: string; valueText: string }>;
}) {
  return (
    <div className="space-y-3">
      <div className="text-sm text-text-3">{stateText}</div>
      {meRows.length > 0 ? (
        <div className="grid gap-2 sm:grid-cols-2">
          {meRows.map((row) => (
            <div key={row.label} className="rounded border border-border bg-surface px-3 py-2">
              <div className="text-xs text-text-3">{row.label}</div>
              <div className="mt-1 text-sm font-medium text-text-1">{row.valueText}</div>
              {helpMode === "full" && getTelemetryFieldHelp(row.label) ? (
                <div className="mt-1 text-xs text-text-3">{getTelemetryFieldHelp(row.label)}</div>
              ) : null}
            </div>
          ))}
        </div>
      ) : null}
      {routingRows.length > 0 ? (
        <div className="grid gap-2 sm:grid-cols-2">
          {routingRows.map((row) => (
            <div key={row.label} className="rounded border border-border bg-surface px-3 py-2">
              <div className="text-xs text-text-3">{row.label}</div>
              <div className="mt-1 text-sm font-mono text-text-1">{row.valueText}</div>
            </div>
          ))}
        </div>
      ) : null}
      {meRows.length === 0 && routingRows.length === 0 ? (
        <div className="text-sm text-text-3">No ME or routing diagnostics are available yet.</div>
      ) : null}
    </div>
  );
}
