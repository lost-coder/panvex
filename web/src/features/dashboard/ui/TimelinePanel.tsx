import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  Badge,
  SectionHeader,
  Timeline,
  cn,
  type DashboardTimelineData,
} from "@/ui";

type EventFilter = "alerts" | "all";

export function TimelinePanel({ data }: Readonly<{ data: DashboardTimelineData }>) {
  const { t } = useTranslation("dashboard");
  // U-26: dev fleets spam routine accept/stop "info" events that bury real
  // problems. Default the dashboard widget to alerts (warn/error) — the full
  // chronological log lives on Activity → Audit — with a toggle to see all.
  const [filter, setFilter] = useState<EventFilter>("alerts");

  const events = useMemo(() => data?.events ?? [], [data]);
  const visible = useMemo(() => {
    const list = filter === "alerts"
      ? events.filter((e) => e.status === "warn" || e.status === "error")
      : events;
    return list.slice(0, 8);
  }, [events, filter]);

  if (!data?.events) return null;

  const filterToggle = (
    <div
      className="inline-flex items-center gap-0.5 p-0.5 rounded-xs border border-border-hi bg-bg"
      role="tablist"
      aria-label={t("timeline.filterLabel")}
    >
      {(["alerts", "all"] as const).map((key) => (
        <button
          key={key}
          type="button"
          role="tab"
          aria-selected={filter === key}
          onClick={() => setFilter(key)}
          className={cn(
            "h-6 px-2 rounded-xs text-nano font-mono transition-colors",
            filter === key ? "bg-bg-card-hi text-fg" : "text-fg-muted hover:text-fg hover:bg-bg-hover",
          )}
        >
          {t(key === "alerts" ? "timeline.filterAlerts" : "timeline.filterAll")}
        </button>
      ))}
    </div>
  );

  return (
    <div className="flex flex-col gap-2 bg-bg-card border border-border rounded-xs p-4">
      <SectionHeader
        title={t("timeline.title")}
        trailing={
          <div className="flex items-center gap-2">
            {filterToggle}
            <Badge variant="accent">{t("timeline.live")}</Badge>
          </div>
        }
      />
      {visible.length === 0 ? (
        <p className="py-6 text-center text-micro text-fg-muted">
          {filter === "alerts" ? t("timeline.allQuiet") : t("timeline.empty")}
        </p>
      ) : (
        <Timeline
          events={visible.map((e) => ({
            status: e.status === "info" ? ("ok" as const) : e.status,
            time: e.time,
            message: e.message,
            source: e.source,
          }))}
        />
      )}
    </div>
  );
}
