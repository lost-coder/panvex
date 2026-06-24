import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";

import { apiClient } from "@/shared/api/api";
import type { EnrollmentAttempt } from "@/shared/api/types-enrollment";
import { resolveConfiguredRootPath } from "@/shared/lib/runtime-path";
import { StatusLabel, type StatusTone } from "@/ui";

import { Fold } from "../server-detail/components/Fold";
import { EnrollmentTimeline } from "./EnrollmentTimeline";

interface Props {
  agentId: string;
}

// Map backend attempt status to the design system's StatusTone so the
// badge stays legible in both light and dark mode. Using StatusLabel
// (instead of raw text-emerald-600 / text-red-600) keeps this in sync
// with how the rest of the panel renders job/audit statuses.
function statusTone(status: EnrollmentAttempt["status"]): StatusTone {
  if (status === "success") return "ok";
  if (status === "failed") return "error";
  return "warn";
}

// EnrollmentHistory renders the most recent enrollment attempts for an
// agent on the Server Detail page. Each row is collapsible; clicking
// fetches the full timeline lazily. The block hides itself when the
// agent has no attempts on record so older agents (enrolled before the
// recorder shipped) don't show a permanently empty section.
export function EnrollmentHistory({ agentId }: Readonly<Props>) {
  const { t } = useTranslation("enrollment");
  const list = useQuery({
    queryKey: ["enrollment-attempts", "by-agent", agentId],
    queryFn: () => apiClient.listEnrollmentAttempts({ agent_id: agentId, limit: 5 }),
  });

  const [expanded, setExpanded] = useState<string | null>(null);
  // Honour the configured panel root path so the deep-link still resolves
  // when the dashboard is mounted under a non-root prefix (e.g. /panvex).
  const rootPath = resolveConfiguredRootPath();
  const viewAllHref = `${rootPath}/enrollment-attempts?agent_id=${encodeURIComponent(agentId)}`;

  const detail = useQuery({
    queryKey: ["enrollment-attempts", "detail", expanded],
    queryFn: () => apiClient.getEnrollmentAttempt(expanded!),
    enabled: !!expanded,
  });

  if (list.isLoading) {
    return (
      <Fold title={t("history.heading")} defaultOpen={false}>
        <div className="text-sm text-fg-muted">{t("history.loading")}</div>
      </Fold>
    );
  }

  if (!list.data || list.data.items.length === 0) {
    return null;
  }

  return (
    <Fold
      title={t("history.heading")}
      rightHint={`${list.data.items.length} ${list.data.items.length === 1 ? "attempt" : "attempts"}`}
      defaultOpen={false}
    >
      <div className="flex flex-col gap-3">
        <ul className="flex flex-col gap-2">
          {list.data.items.map((a: EnrollmentAttempt) => {
            const isOpen = expanded === a.id;
            const label = a.error_code ? `${a.status} (${a.error_code})` : a.status;
            return (
              <li key={a.id} className="rounded-md border border-divider p-3">
                <button
                  type="button"
                  onClick={() => setExpanded(isOpen ? null : a.id)}
                  className="flex w-full items-center justify-between text-left text-sm"
                  aria-expanded={isOpen}
                >
                  <span className="text-fg">
                    {new Date(a.started_at).toLocaleString()} · {a.mode}
                  </span>
                  <StatusLabel tone={statusTone(a.status)} label={label} />
                </button>
                {isOpen && detail.data && (
                  <div className="mt-3">
                    <EnrollmentTimeline detail={detail.data} />
                  </div>
                )}
                {isOpen && detail.isLoading && (
                  <div className="mt-3 text-xs text-fg-muted">
                    {t("history.detailLoading")}
                  </div>
                )}
              </li>
            );
          })}
        </ul>
        {/* Phase-3 §3.b: deep-link into the fleet-wide enrollment
            attempts page, pre-filtered to this agent. Plain <a> keeps
            the dependency surface narrow — the rest of the panel
            doesn't pull tanstack-router's Link, and a full navigation
            here is appropriate (the destination is a lazy-loaded
            route chunk regardless). */}
        <a
          href={viewAllHref}
          className="self-start text-xs text-fg-muted underline"
        >
          {t("history.viewAll")}
        </a>
      </div>
    </Fold>
  );
}
