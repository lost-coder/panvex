import { useTranslation } from "react-i18next";
import { Button } from "@/ui/base/button";

export interface RestartBannerProps {
  pendingFields: string[];
  onRestart: () => void;
  restartInFlight: boolean;
}

export function RestartBanner({ pendingFields, onRestart, restartInFlight }: Readonly<RestartBannerProps>) {
  const { t } = useTranslation("settings");
  if (pendingFields.length === 0) return null;

  const count = pendingFields.length;
  const fieldList = pendingFields.join(", ");

  return (
    <div className="flex items-center justify-between gap-4 rounded-xs border border-status-warn/40 bg-status-warn/10 px-4 py-3 text-sm">
      <p className="text-fg leading-snug">
        <span className="font-semibold text-status-warn">{count}</span>{" "}
        {t("restartBanner.needs", { count })}{" "}
        {t("restartBanner.suffix")}{" "}
        <span className="font-mono text-fg-muted">{fieldList}</span>.
      </p>
      <Button
        size="sm"
        variant="outline"
        onClick={onRestart}
        disabled={restartInFlight}
        className="shrink-0 border-status-warn/50 text-status-warn hover:bg-status-warn/10"
      >
        {restartInFlight ? t("restartBanner.restarting") : t("restartBanner.restartNow")}
      </Button>
    </div>
  );
}
