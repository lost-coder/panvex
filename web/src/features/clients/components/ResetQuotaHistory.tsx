// Phase 3 — reset-quota history Fold for the client detail page. Sources
// audit events from /api/audit (no dedicated endpoint per the plan's
// "reuse existing audit log" decision), filters down to
// `clients.reset_quota` rows for this client, and presents them in a
// collapsed Fold modelled after EnrollmentHistory.

import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";

import { jobsApi } from "@/shared/api/jobs";
import type { AuditEvent } from "@/shared/api/jobs";
import { formatAge } from "@/ui";

import { Fold } from "../../servers/server-detail/components/Fold";

interface Props {
  clientId: string;
  /** Mapping from agent_id -> display label for the row subtitle. */
  agentLabels?: Record<string, string> | undefined;
}

const RESET_QUOTA_ACTION = "clients.reset_quota";

// describeAgents pretty-prints either the fan-out target list or the
// single agent_id from the audit details JSON. The two server-side
// reset paths emit slightly different context shapes:
//
//   - `POST /api/clients/{id}/reset-quota`            → {target_agent_ids: [...], job_id}
//   - `POST /api/clients/{id}/reset-quota/{agentID}`  → {agent_id, job_id}
//
// We accept both and resolve agent IDs to their human label when the
// caller supplied an `agentLabels` map.
function describeAgents(
  details: Record<string, unknown>,
  agentLabels: Record<string, string> | undefined,
): { label: string; count: number } {
  const resolve = (id: string) => agentLabels?.[id] ?? id;
  const targets = details["target_agent_ids"];
  if (Array.isArray(targets)) {
    const ids = targets.filter((v): v is string => typeof v === "string");
    return {
      label: ids.map(resolve).join(", "),
      count: ids.length,
    };
  }
  const single = details["agent_id"];
  if (typeof single === "string" && single !== "") {
    return { label: resolve(single), count: 1 };
  }
  return { label: "", count: 0 };
}

export function ResetQuotaHistory({ clientId, agentLabels }: Readonly<Props>) {
  const { t } = useTranslation("clients");

  // The /api/audit ring buffer is server-wide, so we filter client-side.
  // The ring is small (most operators see <500 events) and lazy-loading
  // a per-client cursor view would be a Phase-4 follow-up if the ring
  // grows past one screen of resets. For now, in-browser filter keeps
  // the implementation slim and avoids a new backend endpoint.
  const list = useQuery({
    queryKey: ["audit", "client-reset-quota", clientId],
    queryFn: jobsApi.audit,
  });

  if (list.isLoading) {
    return (
      <Fold title={t("detail.quota.history.heading")} defaultOpen={false}>
        <div className="text-sm text-fg-muted">{t("detail.quota.history.loading")}</div>
      </Fold>
    );
  }
  if (!list.data) {
    return null;
  }

  const events: AuditEvent[] = list.data
    .filter((e) => e.action === RESET_QUOTA_ACTION && e.target_id === clientId)
    // The /api/audit ring is chronological-ascending (timeline replay);
    // operators expect "most recent first" when browsing history, so
    // flip the slice before rendering.
    .slice()
    .reverse();

  // Hide entirely on empty — same pattern as EnrollmentHistory — so a
  // client that has never been reset doesn't carry a permanently empty
  // section.
  if (events.length === 0) {
    return null;
  }

  return (
    <Fold
      title={t("detail.quota.history.heading")}
      rightHint={String(events.length)}
      defaultOpen={false}
    >
      <ul className="flex flex-col gap-2">
        {events.map((event) => {
          const when = new Date(event.created_at);
          const ageLabel = formatAge(Math.floor(when.getTime() / 1000));
          const agents = describeAgents(event.details, agentLabels);
          const headline =
            agents.count > 1
              ? t("detail.quota.history.rowFanout", { count: agents.count })
              : t("detail.quota.history.rowSingle", { agent: agents.label || "—" });
          return (
            <li
              key={event.id}
              className="rounded-md border border-divider p-3 flex flex-col gap-1"
            >
              <div className="flex items-center justify-between gap-2">
                <span className="text-sm text-fg">{headline}</span>
                <span className="text-micro font-mono text-fg-muted tabular-nums">
                  {when.toLocaleString()} · {ageLabel}
                </span>
              </div>
              <div className="flex items-center gap-3 text-micro font-mono text-fg-muted">
                <span>{t("detail.quota.history.by", { actor: event.actor_id })}</span>
                {agents.count > 1 && agents.label !== "" && (
                  <span className="truncate">{agents.label}</span>
                )}
              </div>
            </li>
          );
        })}
      </ul>
    </Fold>
  );
}
