import { Button } from "@/ui/base/button";

export interface RestartBannerProps {
  pendingFields: string[];
  onRestart: () => void;
  restartInFlight: boolean;
}

export function RestartBanner({ pendingFields, onRestart, restartInFlight }: Readonly<RestartBannerProps>) {
  if (pendingFields.length === 0) return null;

  const count = pendingFields.length;
  const fieldList = pendingFields.join(", ");

  return (
    <div className="flex items-center justify-between gap-4 rounded-xs border border-status-warn/40 bg-status-warn/10 px-4 py-3 text-sm">
      <p className="text-fg leading-snug">
        <span className="font-semibold text-status-warn">{count}</span>{" "}
        {count === 1 ? "setting needs" : "settings need"} a panel restart to
        take effect:{" "}
        <span className="font-mono text-fg-muted">{fieldList}</span>.
      </p>
      <Button
        size="sm"
        variant="outline"
        onClick={onRestart}
        disabled={restartInFlight}
        className="shrink-0 border-status-warn/50 text-status-warn hover:bg-status-warn/10"
      >
        {restartInFlight ? "Restarting…" : "Restart now"}
      </Button>
    </div>
  );
}
