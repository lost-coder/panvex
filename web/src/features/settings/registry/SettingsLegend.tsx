import { useTranslation } from "react-i18next";
import { cn } from "@/ui/lib/cn";
import type { IndicatorTone } from "./indicators";
import { IndicatorIcon } from "./IndicatorIcon";

interface LegendItemProps {
  bar?: IndicatorTone;
  icon: "lock" | "restart";
  tone: IndicatorTone;
  spinning?: boolean;
  label: string;
}

function LegendItem({ bar, icon, tone, spinning, label }: Readonly<LegendItemProps>) {
  return (
    <span className="inline-flex items-center gap-1.5">
      {bar && (
        <span
          aria-hidden
          className={cn("inline-block h-4 w-1 rounded-xs", bar === "amber" ? "bg-status-warn" : "bg-border-hi")}
        />
      )}
      <IndicatorIcon icon={icon} tone={tone} {...(spinning ? { spinning } : {})} />
      <span>{label}</span>
    </span>
  );
}

// One-line key explaining the per-field indicators. Rendered once near the top
// of the settings page when schema-driven fields are present.
export function SettingsLegend() {
  const { t } = useTranslation("settings");
  return (
    <div className="flex flex-wrap items-center gap-x-5 gap-y-2 rounded-xs border border-border bg-bg-card px-4 py-2.5 text-xs text-fg-muted">
      <LegendItem bar="grey" icon="lock" tone="grey" label={t("settingsLegend.configManaged")} />
      <LegendItem bar="amber" icon="lock" tone="amber" label={t("settingsLegend.envOverride")} />
      <LegendItem bar="amber" icon="restart" tone="amber" label={t("settingsLegend.needsRestart")} />
      <LegendItem icon="restart" tone="amber" spinning label={t("settingsLegend.pendingRestart")} />
    </div>
  );
}
