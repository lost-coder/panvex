import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import type { RuntimeEvent, RuntimeEventLevel } from "@/shared/api/types-runtime-events";

import { Fold } from "../server-detail/components/Fold";
import { useAgentRuntimeEvents } from "./useAgentRuntimeEvents";

interface Props {
  agentId: string;
}

const ALL_LEVELS: RuntimeEventLevel[] = ["info", "warn", "error"];

function levelClass(lvl: RuntimeEventLevel): string {
  switch (lvl) {
    case "error":
      return "text-status-error";
    case "warn":
      return "text-status-warn";
    default:
      return "text-status-ok";
  }
}

function iconFor(lvl: RuntimeEventLevel): string {
  switch (lvl) {
    case "error":
      return "✕";
    case "warn":
      return "!";
    default:
      return "·";
  }
}

export function RuntimeEvents({ agentId }: Props) {
  const { t } = useTranslation("runtime-events");
  const { events, isLoading, isLive } = useAgentRuntimeEvents(agentId);
  const [enabled, setEnabled] = useState<Set<RuntimeEventLevel>>(new Set(ALL_LEVELS));
  const [expanded, setExpanded] = useState<number | null>(null);

  const visible = useMemo(
    () => events.filter((e: RuntimeEvent) => enabled.has(e.level)),
    [events, enabled],
  );

  const toggle = (lvl: RuntimeEventLevel) => {
    setEnabled((prev) => {
      const next = new Set(prev);
      if (next.has(lvl)) next.delete(lvl);
      else next.add(lvl);
      return next;
    });
  };

  const rightHint = isLive ? (
    <span className="inline-flex items-center gap-1 text-xs text-status-ok">
      <span className="inline-block h-2 w-2 rounded-full bg-status-ok animate-pulse" />
      {t("section.live")}
    </span>
  ) : null;

  return (
    <Fold title={t("section.title")} rightHint={rightHint} defaultOpen={false}>
      <div className="flex flex-col gap-3">
        <div className="flex gap-2">
          {ALL_LEVELS.map((lvl) => (
            <button
              key={lvl}
              type="button"
              onClick={() => toggle(lvl)}
              aria-pressed={enabled.has(lvl)}
              className={`rounded-md border border-divider px-3 py-1 text-xs ${
                enabled.has(lvl) ? "bg-bg-card text-fg" : "text-fg-muted"
              }`}
            >
              {t(`filters.${lvl}`)}
            </button>
          ))}
        </div>

        {isLoading && <div className="text-sm text-fg-muted">{"…"}</div>}
        {!isLoading && visible.length === 0 && (
          <div className="text-sm text-fg-muted">{t("section.empty")}</div>
        )}
        <ol className="flex flex-col gap-2">
          {visible.map((e, idx) => {
            const isExpanded = expanded === idx;
            const hasFields = !!e.fields && Object.keys(e.fields).length > 0;
            return (
              <li key={`${e.ts}-${idx}`} className="rounded-md border border-divider p-2">
                <div className="flex items-start gap-3">
                  <span
                    className={`mt-0.5 inline-flex h-5 w-5 items-center justify-center rounded-full ${levelClass(e.level)}`}
                  >
                    {iconFor(e.level)}
                  </span>
                  <div className="flex flex-1 flex-col">
                    <div className="text-sm font-medium text-fg">{e.message}</div>
                    <div className="text-xs text-fg-muted">
                      {new Date(e.ts).toLocaleTimeString()}
                    </div>
                    {hasFields && (
                      <button
                        type="button"
                        onClick={() => setExpanded(isExpanded ? null : idx)}
                        className="mt-1 text-xs text-fg-muted underline"
                      >
                        {isExpanded ? t("row.fieldsCollapse") : t("row.fieldsExpand")}
                      </button>
                    )}
                    {isExpanded && hasFields && (
                      <pre className="mt-1 max-h-40 overflow-auto rounded bg-bg-card p-2 text-xs text-fg-muted">
                        {JSON.stringify(e.fields, null, 2)}
                      </pre>
                    )}
                  </div>
                </div>
              </li>
            );
          })}
        </ol>
      </div>
    </Fold>
  );
}
