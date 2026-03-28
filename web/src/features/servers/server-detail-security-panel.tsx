import { getTelemetryFieldHelp, type TelemetryHelpMode } from "@/features/telemetry/help-metadata";

export function ServerDetailSecurityPanel({
  stateText,
  helpMode,
  rows,
  entries,
}: {
  stateText: string;
  helpMode: TelemetryHelpMode;
  rows: Array<{ label: string; valueText: string }>;
  entries: string[];
}) {
  return (
    <div className="space-y-3">
      <div className="text-sm text-text-3">{stateText}</div>
      {rows.length === 0 ? (
        <div className="text-sm text-text-3">No security values are available yet.</div>
      ) : (
        <div className="grid gap-2 sm:grid-cols-2">
          {rows.map((row) => (
            <div key={row.label} className="rounded border border-border bg-surface px-3 py-2">
              <div className="text-xs text-text-3">{row.label}</div>
              <div className="mt-1 text-sm font-medium text-text-1">{row.valueText}</div>
              {helpMode === "full" && getTelemetryFieldHelp(row.label) ? (
                <div className="mt-1 text-xs text-text-3">{getTelemetryFieldHelp(row.label)}</div>
              ) : null}
            </div>
          ))}
        </div>
      )}
      {entries.length > 0 ? (
        <div className="rounded border border-border bg-surface px-3 py-2">
          <div className="text-xs text-text-3">Whitelist Entries</div>
          <div className="mt-2 flex flex-wrap gap-2">
            {entries.map((entry) => (
              <span key={entry} className="rounded bg-surface-raised px-2 py-1 text-xs font-mono text-text-2">
                {entry}
              </span>
            ))}
          </div>
        </div>
      ) : null}
    </div>
  );
}
